# Linux Docker 无损升级手册

本文用于在 Linux 服务器上安全升级 iCloud HME Docker 部署，并保留全部持久化数据。

## 1. 必须保留的数据

容器内的持久化目录固定为 `/app/data`。仓库的 `compose.yaml` 使用 bind mount，将宿主机的 `ICLOUD_HME_DATA_DIR` 映射到该目录。生产环境必须使用仓库外的绝对路径，例如 `/srv/icloud-hme/data`。

需要备份整个数据目录，而不只是 `accounts.json`：

| 路径 | 内容 |
| --- | --- |
| `accounts.json` | 账号、Cookie、App Password、代理、自动化状态、每账号最近 500 条创建批次历史 |
| `platform-auth.json` | 管理员用户名和密码哈希 |
| `email-notification.json` | 邮件通知配置和授权码 |
| `webhook-notification.json` | Webhook URL 和签名密钥 |
| `operation-logs/*.jsonl` | 最近 7 天操作日志 |

浏览器登录会话只在内存中，容器重启后需要重新登录是正常现象。邮件正文和真实 Hide My Email 别名位于 Apple 服务端，本地备份不能回滚已经对 Apple 账号执行的创建、删除或停用操作。

升级期间不要执行：

```text
docker compose down -v
docker system prune --volumes
rm -rf /srv/icloud-hme/data
```

## 2. 设置本次升级变量

以下命令使用 Bash。先将路径和版本改成服务器的真实值，并尽量在同一个 shell 中完成整个流程：

```bash
set -euo pipefail

export APP_DIR='/opt/icloud-hme'
export DATA_DIR='/srv/icloud-hme/data'
export BACKUP_DIR='/srv/icloud-hme/backups'
export ENV_FILE="$APP_DIR/.env"
export COMPOSE_FILE="$APP_DIR/compose.yaml"
export NEW_VERSION='vX.Y.Z'
export NEW_IMAGE="ghcr.io/zhaohao0924/icloud-hme-mng:${NEW_VERSION}"
export UPGRADE_ID="$(date -u +%Y%m%dT%H%M%SZ)"

cd "$APP_DIR"
```

`vX.Y.Z` 是占位符。生产环境应固定到已经发布的 `v*` 标签，不建议使用会移动的 `latest` 或 `main`。

## 3. 升级前核对

### 3.1 确认当前容器和真实挂载路径

`config --quiet` 只校验配置，不打印展开后的 API Token：

```bash
docker version
docker compose version
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" config --quiet

OLD_CONTAINER_ID="$(docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" ps -q app)"
test -n "$OLD_CONTAINER_ID"
docker inspect --format '{{.State.Status}} {{.Config.Image}}' "$OLD_CONTAINER_ID"
MOUNT_TYPE="$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/app/data"}}{{.Type}}{{end}}{{end}}' "$OLD_CONTAINER_ID")"
MOUNT_SOURCE="$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/app/data"}}{{.Source}}{{end}}{{end}}' "$OLD_CONTAINER_ID")"
test "$MOUNT_TYPE" = 'bind'
test "$MOUNT_SOURCE" = "$DATA_DIR"
printf 'type=%s source=%s target=/app/data\n' "$MOUNT_TYPE" "$MOUNT_SOURCE"
```

最后一条命令必须显示 `type=bind source=/srv/icloud-hme/data target=/app/data`。如果 `source` 与 `DATA_DIR` 不一致，立即停止升级并查清部署配置。不能让新版本因工作目录变化而使用新的空 `./data`。

只检查非密钥配置和文件名，不读取文件内容：

```bash
grep -E '^(ICLOUD_HME_DATA_DIR|ICLOUD_HME_IMAGE|ICLOUD_HME_VERSION|ICLOUD_HME_BIND_ADDRESS|ICLOUD_HME_PORT|TZ)=' "$ENV_FILE"
sudo test -d "$DATA_DIR"
sudo test -s "$DATA_DIR/accounts.json"
sudo find "$DATA_DIR" -maxdepth 2 -type f -printf '%P\t%s bytes\n' | sort
if sudo find "$DATA_DIR" -type l -print -quit | grep -q .; then
  echo '数据目录不能包含符号链接，请先核对真实数据边界' >&2
  exit 1
fi
```

若已有账号的实例缺少 `accounts.json`，或该文件意外为空，不要启动新版本。

### 3.2 保留旧镜像

```bash
OLD_IMAGE_ID="$(docker inspect --format '{{.Image}}' "$OLD_CONTAINER_ID")"
OLD_IMAGE_REF="$(docker inspect --format '{{.Config.Image}}' "$OLD_CONTAINER_ID")"
ROLLBACK_IMAGE="icloud-hme:rollback-${UPGRADE_ID}"

docker image tag "$OLD_IMAGE_ID" "$ROLLBACK_IMAGE"
docker image inspect "$ROLLBACK_IMAGE" --format 'rollback image={{.Id}} os={{.Os}} arch={{.Architecture}}'
```

这个本地标签保证旧镜像原标签被覆盖后仍能回滚。升级验收完成前不要删除它。

### 3.3 备份部署配置

这一步必须在源码模式执行 `git checkout` 之前完成：

```bash
sudo mkdir -p "$BACKUP_DIR"
sudo chmod 700 "$BACKUP_DIR"

ENV_BACKUP="$BACKUP_DIR/icloud-hme-env-${UPGRADE_ID}"
COMPOSE_BACKUP="$BACKUP_DIR/icloud-hme-compose-${UPGRADE_ID}.yaml"

sudo test ! -e "$ENV_BACKUP"
sudo test ! -e "$COMPOSE_BACKUP"
sudo cp --archive "$ENV_FILE" "$ENV_BACKUP"
sudo cp --archive "$COMPOSE_FILE" "$COMPOSE_BACKUP"
sudo chmod 600 "$ENV_BACKUP"
```

### 3.4 记录只读基线

以下检查需要 `curl` 和 `jq`，且不会输出 API Token：

```bash
API_TOKEN="$(sed -n 's/^ICLOUD_HME_API_TOKEN=//p' "$ENV_FILE")"
API_PORT="$(sed -n 's/^ICLOUD_HME_PORT=//p' "$ENV_FILE")"
API_PORT="${API_PORT:-8081}"
BASE_URL="http://127.0.0.1:${API_PORT}"
test "${#API_TOKEN}" -ge 32

BEFORE_ACCOUNT_COUNT="$(
  curl -fsS \
    -H "Authorization: Bearer ${API_TOKEN}" \
    "$BASE_URL/api/accounts" |
  jq -er '.data | length'
)"
BEFORE_ACCOUNTS_SHA="$(sudo sha256sum "$DATA_DIR/accounts.json" | awk '{print $1}')"
unset API_TOKEN

printf 'account_count=%s\naccounts_sha256=%s\nold_image_ref=%s\nold_image_id=%s\nrollback_image=%s\n' \
  "$BEFORE_ACCOUNT_COUNT" "$BEFORE_ACCOUNTS_SHA" "$OLD_IMAGE_REF" "$OLD_IMAGE_ID" "$ROLLBACK_IMAGE"
```

保存输出，升级后将核对账号数量和主数据文件哈希。

## 4. 停机前准备新镜像

下面两种方式二选一。

### 4.1 推荐：拉取已发布的 GHCR 镜像

```bash
docker pull "$NEW_IMAGE"
docker image inspect "$NEW_IMAGE" --format 'new image={{.Id}} os={{.Os}} arch={{.Architecture}} digests={{json .RepoDigests}}'
```

发布镜像支持 `linux/amd64` 和 `linux/arm64`。仓库的 Compose 服务同时声明了 `build` 和 `image`；使用发布镜像时，启动命令必须带 `--no-build`。使用 `--build` 会构建服务器上的本地源码，而不是使用刚拉取的发布镜像。

### 4.2 可选：从服务器上的源码构建

```bash
cd "$APP_DIR"
git status --short
git fetch --tags
git checkout "$NEW_VERSION"
docker build --pull \
  --build-arg VERSION="$NEW_VERSION" \
  --tag "icloud-hme:${NEW_VERSION}" \
  .

export NEW_IMAGE="icloud-hme:${NEW_VERSION}"
docker image inspect "$NEW_IMAGE" --format 'new image={{.Id}} os={{.Os}} arch={{.Architecture}}'
```

如果 `git status --short` 有输出，先确认本地改动的归属，不要强制覆盖。独立版本标签可以避免覆盖旧镜像。

## 5. 进入维护窗口

1. 在 Web UI 记录升级前处于启用状态的自动化规则。
2. 暂停这些规则，并等待正在执行的任务完成。
3. 停止用户修改配置。
4. 停止容器并确认它不再运行。

```bash
docker stop --time 30 "$OLD_CONTAINER_ID" >/dev/null
test "$(docker inspect --format '{{.State.Running}}' "$OLD_CONTAINER_ID")" = 'false'
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" ps --all
```

只有确认 `Running=false` 后才能备份。暂停规则会写入 `accounts.json`，因此停止容器后重新记录最终哈希：

```bash
BEFORE_ACCOUNTS_SHA="$(sudo sha256sum "$DATA_DIR/accounts.json" | awk '{print $1}')"
```

## 6. 创建并校验离线备份

备份目录必须在数据目录之外，最好位于另一块磁盘，并由 root 独占访问：

```bash
ARCHIVE="$BACKUP_DIR/icloud-hme-data-${UPGRADE_ID}.tar.gz"

case "$BACKUP_DIR/" in
  "$DATA_DIR/"*) echo '备份目录不能位于数据目录内' >&2; exit 1 ;;
esac
sudo test ! -e "$ARCHIVE"
sudo test ! -e "${ARCHIVE}.sha256"
sudo du -sh "$DATA_DIR"
sudo df -h "$BACKUP_DIR"

DATA_PARENT="$(dirname "$DATA_DIR")"
DATA_NAME="$(basename "$DATA_DIR")"
test "$DATA_DIR" != '/'
test "$DATA_NAME" != '.'

sudo tar \
  --acls \
  --xattrs \
  --numeric-owner \
  --one-file-system \
  -C "$DATA_PARENT" \
  -czf "$ARCHIVE" \
  "$DATA_NAME"

sudo chmod 600 "$ARCHIVE"
ARCHIVE_SHA256="$(sudo sha256sum "$ARCHIVE" | awk '{print $1}')"
printf '%s\n' "$ARCHIVE_SHA256" | sudo tee "${ARCHIVE}.sha256" >/dev/null
sudo chmod 600 "${ARCHIVE}.sha256"
```

归档从数据目录的父目录创建，因此保留顶层目录名、权限和所有数据文件。`.env` 和归档都包含敏感信息，不能上传到公开网盘、聊天或工单。

以下校验必须全部成功：压缩流完整、SHA-256 匹配、目录可读取、归档中的 `accounts.json` 是合法 JSON。

```bash
sudo gzip -t "$ARCHIVE"
test "$(sudo sha256sum "$ARCHIVE" | awk '{print $1}')" = "$(sudo cat "${ARCHIVE}.sha256")"
sudo tar -tzf "$ARCHIVE" >/dev/null
sudo tar -xOzf "$ARCHIVE" "$DATA_NAME/accounts.json" | jq -e empty >/dev/null

sudo tar -tzf "$ARCHIVE" | grep -Fxq "$DATA_NAME/accounts.json"
if sudo test -f "$DATA_DIR/platform-auth.json"; then
  sudo tar -tzf "$ARCHIVE" | grep -Fxq "$DATA_NAME/platform-auth.json"
fi
```

还应把归档和 `.sha256` 文件复制到另一台受控主机或对象存储，并在那里再次比较 SHA-256。只有本机一份副本无法防止服务器磁盘故障。

## 7. 切换到新版本

用 `sudoedit "$ENV_FILE"` 只修改以下两个值；`ICLOUD_HME_DATA_DIR`、API Token、端口和时区必须保持不变：

```dotenv
ICLOUD_HME_IMAGE=ghcr.io/zhaohao0924/icloud-hme-mng:vX.Y.Z
ICLOUD_HME_VERSION=vX.Y.Z
ICLOUD_HME_DATA_DIR=/srv/icloud-hme/data
```

源码构建模式把 `ICLOUD_HME_IMAGE` 改为 `icloud-hme:vX.Y.Z`。随后校验配置并仅重建应用容器：

```bash
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" config --quiet
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" \
  up -d --no-build --force-recreate --wait --wait-timeout 120 app
```

启动失败时不要改用空数据目录，也不要初始化新管理员；直接查看日志并按第 9 节回滚。

若当前 Compose 不支持 `--wait`，先去掉这两个 wait 参数启动，再用 `docker compose ps` 轮询，直到健康状态为 `healthy`。

## 8. 升级后只读验收

先再次确认新容器仍挂载同一个宿主机目录：

```bash
NEW_CONTAINER_ID="$(docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" ps -q app)"
test -n "$NEW_CONTAINER_ID"

docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" ps
MOUNT_TYPE="$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/app/data"}}{{.Type}}{{end}}{{end}}' "$NEW_CONTAINER_ID")"
MOUNT_SOURCE="$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/app/data"}}{{.Source}}{{end}}{{end}}' "$NEW_CONTAINER_ID")"
test "$MOUNT_TYPE" = 'bind'
test "$MOUNT_SOURCE" = "$DATA_DIR"
printf 'type=%s source=%s target=/app/data\n' "$MOUNT_TYPE" "$MOUNT_SOURCE"
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" logs --tail 200 app
```

日志应显示预期版本、`data_dir=/app/data` 和正确账号数量，不应有 JSON 解析、权限或初始化错误。然后验证带数据配置检查的健康接口：

```bash
API_TOKEN="$(sed -n 's/^ICLOUD_HME_API_TOKEN=//p' "$ENV_FILE")"
API_PORT="$(sed -n 's/^ICLOUD_HME_PORT=//p' "$ENV_FILE")"
API_PORT="${API_PORT:-8081}"
BASE_URL="http://127.0.0.1:${API_PORT}"
test "${#API_TOKEN}" -ge 32

curl -fsS \
  -H "Authorization: Bearer ${API_TOKEN}" \
  "$BASE_URL/api/health" |
jq -e --arg version "$NEW_VERSION" '
  .success == true and
  .data.status == "ok" and
  .data.config_available == true and
  .data.version == $version
'
```

在任何写操作前比较账号数量和主数据文件：

```bash
AFTER_ACCOUNT_COUNT="$(
  curl -fsS \
    -H "Authorization: Bearer ${API_TOKEN}" \
    "$BASE_URL/api/accounts" |
  jq -er '.data | length'
)"
AFTER_ACCOUNTS_SHA="$(sudo sha256sum "$DATA_DIR/accounts.json" | awk '{print $1}')"

test "$AFTER_ACCOUNT_COUNT" = "$BEFORE_ACCOUNT_COUNT"
test "$AFTER_ACCOUNTS_SHA" = "$BEFORE_ACCOUNTS_SHA"
unset API_TOKEN
```

随后在 Web UI 检查：

1. 页面要求使用原管理员登录，而不是重新初始化管理员。
2. 账号数量、名称和状态正确。
3. 自动化配置、累计进度、下次执行时间和创建历史存在，且规则仍暂停。
4. 邮件通知、Webhook 和操作日志仍可读取。
5. 可以使用自动化“预览”；预览不会创建别名或写入规则历史。

不要用创建、删除、停用别名或删除账号作为验收，这些操作会改变 Apple 服务端状态。观察一段时间没有错误后，只恢复升级前确实启用的自动化规则。

## 9. 回滚

### 9.1 仅回滚镜像

适用于新版本没有进行配置写入，且旧版本能读取当前数据格式的情况：

```bash
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" stop -t 30 app
sudoedit "$ENV_FILE"
```

将 `ICLOUD_HME_IMAGE` 改为第 3.2 节的本地标签，例如 `icloud-hme:rollback-YYYYMMDDTHHMMSSZ`，再启动并重复第 8 节检查：

```bash
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" config --quiet
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" \
  up -d --no-build --force-recreate --wait --wait-timeout 120 app
```

### 9.2 镜像和数据一起回滚

如果新版本已经修改数据、出现格式兼容问题，或无法确认数据是否被写入，应恢复升级前离线备份。恢复会放弃备份时间点之后的本地改动，因此验收阶段必须保持自动化暂停并禁止配置写入。

```bash
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" stop -t 30 app
test "$(sudo sha256sum "$ARCHIVE" | awk '{print $1}')" = "$(sudo cat "${ARCHIVE}.sha256")"

ROLLBACK_AT="$(date -u +%Y%m%dT%H%M%SZ)"
FAILED_DATA_DIR="${DATA_DIR}.failed-${ROLLBACK_AT}"
test "$DATA_DIR" != '/'
test ! -e "$FAILED_DATA_DIR"
sudo mv -- "$DATA_DIR" "$FAILED_DATA_DIR"
sudo tar -xzf "$ARCHIVE" -C "$DATA_PARENT"
sudo test -s "$DATA_DIR/accounts.json"
sudo chmod 700 "$DATA_DIR"
```

这里通过重命名保留新版本的数据目录，没有删除它。如果源码升级改变了 `compose.yaml`，先恢复旧文件：

```bash
sudo cp --archive --force "$COMPOSE_BACKUP" "$COMPOSE_FILE"
```

然后恢复升级前 `.env`；如果原镜像标签已经被覆盖，再把 `ICLOUD_HME_IMAGE` 改为 `ROLLBACK_IMAGE`：

```bash
sudo cp --archive --force "$ENV_BACKUP" "$ENV_FILE"
sudoedit "$ENV_FILE"

docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" config --quiet
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" \
  up -d --no-build --force-recreate --wait --wait-timeout 120 app
```

回滚验收完成前，不要删除 `FAILED_DATA_DIR`、备份归档或旧镜像。

## 10. 完成标准

- 新容器是 `healthy`，镜像和目标版本一致。
- `/app/data` 仍映射到升级前的同一宿主机绝对路径。
- `/api/health` 返回 `status=ok`、`config_available=true` 和目标版本。
- 账号数量与升级前一致，首次只读检查前 `accounts.json` 哈希一致。
- 原管理员可登录，自动化配置、创建历史、通知配置和操作日志均存在。
- 已验证的离线备份已经复制到独立存储。
- 自动化只在全部检查通过后按升级前状态恢复。

注意：操作日志按程序设计只保留最近 7 天，账号创建历史只保留每个账号最近 500 个批次。升级能完整保留当前数据目录，但不会改变这两个内置保留策略。
