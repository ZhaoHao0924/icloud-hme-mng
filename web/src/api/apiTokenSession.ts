let currentApiToken: string | undefined;

export function clearApiToken() {
  currentApiToken = undefined;
}

export function getApiToken() {
  return currentApiToken;
}

export function setApiToken(token: string) {
  currentApiToken = token;
}
