type CredentialCapabilityStatusProps = {
  configured: boolean;
  configuredLabel?: string;
  missingLabel?: string;
};

export function CredentialCapabilityStatus({
  configured,
  configuredLabel = "已配置",
  missingLabel = "未配置",
}: CredentialCapabilityStatusProps) {
  return (
    <span
      className={`capability-tag ${configured ? "capability-configured" : "capability-missing"}`}
    >
      <span className="capability-dot" aria-hidden="true" />
      {configured ? configuredLabel : missingLabel}
    </span>
  );
}
