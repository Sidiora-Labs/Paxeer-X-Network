import { FC, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { VaultInfo } from "../../types/vault";
import { VaultCardWidget } from "../vault-card";
import { VaultsList } from "./vaults-list";
import { ViewModeToggle, ViewMode } from "./view-mode-toggle";

export const AllVaultsDesktop: FC<{ vaults: VaultInfo[] }> = (props) => {
  const { vaults } = props;
  const { t } = useTranslation();
  const [viewMode, setViewMode] = useState<ViewMode>("grid");

  return (
    <div className="oui-flex oui-flex-col oui-gap-6">
      <div className="oui-flex oui-items-center oui-justify-between">
        <div className="oui-text-xl oui-font-normal oui-text-base-contrast">
          {t("vaults.allVaults")}
        </div>
        <ViewModeToggle mode={viewMode} onChange={setViewMode} />
      </div>
      {viewMode === "grid" ? (
        <div className="oui-grid oui-grid-cols-3 oui-gap-4 min-[1024px]:oui-grid-cols-3">
          {vaults.map((vault) => (
            <VaultCardWidget key={vault.vault_id} vault={vault} />
          ))}
        </div>
      ) : (
        <VaultsList vaults={vaults} />
      )}
    </div>
  );
};
