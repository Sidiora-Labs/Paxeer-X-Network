import { FC } from "react";
import { cn } from "@paxeer-network/ui";

export type ViewMode = "grid" | "list";

interface ViewModeToggleProps {
  mode: ViewMode;
  onChange: (mode: ViewMode) => void;
}

export const ViewModeToggle: FC<ViewModeToggleProps> = ({ mode, onChange }) => {
  const toggleMode = () => {
    onChange(mode === "grid" ? "list" : "grid");
  };

  return (
    <button
      onClick={toggleMode}
      className="oui-flex oui-h-8 oui-w-8 oui-items-center oui-justify-center oui-rounded oui-transition-colors hover:oui-bg-base-6"
      aria-label={
        mode === "grid" ? "Switch to list view" : "Switch to grid view"
      }
    >
      {mode === "grid" ? (
        <svg
          width="18"
          height="18"
          viewBox="0 0 18 18"
          fill="none"
          xmlns="http://www.w3.org/2000/svg"
        >
          <path
            d="M2 4h14v2H2V4zm0 4h14v2H2V8zm0 4h14v2H2v-2z"
            fill="currentColor"
            opacity="0.54"
          />
        </svg>
      ) : (
        <svg
          width="18"
          height="18"
          viewBox="0 0 18 18"
          fill="none"
          xmlns="http://www.w3.org/2000/svg"
        >
          <path
            d="M2 2h6v6H2V2zm0 8h6v6H2v-6zm8-8h6v6h-6V2zm0 8h6v6h-6v-6z"
            fill="currentColor"
            opacity="0.54"
          />
        </svg>
      )}
    </button>
  );
};
