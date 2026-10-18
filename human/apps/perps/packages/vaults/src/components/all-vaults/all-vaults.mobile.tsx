import { FC } from "react";
import { VaultInfo } from "../../types/vault";
import { VaultCardWidget } from "../vault-card";

export const AllVaultsMobile: FC<{ vaults: VaultInfo[] }> = (props) => {
  const { vaults } = props;

  return (
    <div className="oui-flex oui-flex-col oui-gap-3">
      {vaults.map((vault) => (
        <VaultCardWidget key={vault.vault_id} vault={vault} />
      ))}
    </div>
  );
};
