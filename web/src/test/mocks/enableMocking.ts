export async function enableMocking() {
  if (!import.meta.env.DEV || import.meta.env.VITE_ENABLE_MSW !== "true") {
    return;
  }

  const { worker } = await import("./browser");
  await worker.start({
    onUnhandledRequest: "bypass",
    quiet: true,
    serviceWorker: {
      url: "/mockServiceWorker.js",
    },
  });
}
