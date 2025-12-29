// Vite needs explicit worker wiring for monaco-editor.
// Without this, Monaco falls back to running workers on the main thread.
import EditorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";
import JsonWorker from "monaco-editor/esm/vs/language/json/json.worker?worker";

export function ensureMonacoWorkers(): void {
  const g = globalThis as any;

  if (g.MonacoEnvironment?.getWorker) {
    return;
  }

  g.MonacoEnvironment = {
    getWorker(_moduleId: string, label: string) {
      switch (label) {
        case "json":
          return new JsonWorker();
        default:
          return new EditorWorker();
      }
    },
  };
}
