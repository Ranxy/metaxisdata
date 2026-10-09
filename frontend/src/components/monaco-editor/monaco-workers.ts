// Vite needs explicit worker wiring for monaco-editor.
// Without this, Monaco falls back to running workers on the main thread.
// The base editor worker is the only one left: no language services are bundled.
import EditorWorker from "monaco-editor/editor/editor.worker?worker";

export function ensureMonacoWorkers(): void {
  const g = globalThis as any;

  if (g.MonacoEnvironment?.getWorker) {
    return;
  }

  g.MonacoEnvironment = {
    getWorker() {
      return new EditorWorker();
    },
  };
}
