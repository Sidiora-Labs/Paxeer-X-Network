import { createContext, useContext } from "react";

export type Theme = Record<string, string>;

type ViewMode = "visual" | "code";

export type EditorContextState = {
  theme: Theme;
  mode: ViewMode;
  setTheme: (theme: Theme, changedValues?: Record<string, string>) => void;
  setMode: (mode: ViewMode) => void;
  resetTheme: () => void;
  loading: boolean;
};

export const EditorContext = createContext<EditorContextState>({
  theme: {},
  mode: "visual",
  setTheme: (theme: Theme, changedValues?: Record<string, string>) => {},
  setMode: (mode: ViewMode) => {},
  resetTheme: () => {},
  loading: false,
});

export const useTheme = () => useContext(EditorContext);
