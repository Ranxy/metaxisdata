import type * as monaco from "monaco-editor";

let monacoModule: typeof monaco | undefined;
let loadPromise: Promise<typeof monaco> | undefined;

export async function loadMonacoEditor(): Promise<typeof monaco> {
  if (monacoModule) {
    return monacoModule;
  }

  if (loadPromise) {
    return loadPromise;
  }

  loadPromise = import("monaco-editor").then((module) => {
    monacoModule = module;
    return module;
  });

  return loadPromise;
}

export async function getMonacoEditor(): Promise<typeof monaco> {
  if (monacoModule) {
    return monacoModule;
  }
  return loadMonacoEditor();
}

export function isMonacoLoaded(): boolean {
  return monacoModule !== undefined;
}
