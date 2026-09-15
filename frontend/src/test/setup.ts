// jsdom does not implement `window.matchMedia`, which the responsive shell and
// the theme resolver both read. A non-matching stub keeps component tests on the
// expanded-desktop code path.
//
// The guard matters because some tests (the locale scripts) run in the node
// environment, where this setup file is still evaluated.
if (typeof window !== "undefined" && !window.matchMedia) {
  window.matchMedia = (query: string): MediaQueryList =>
    ({
      matches: false,
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => false,
    }) as unknown as MediaQueryList;
}
