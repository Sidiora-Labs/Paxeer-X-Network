import { useTranslation } from "@paxeer-network/i18n";
import { Button } from "@paxeer-network/ui";
import { FaucetState } from "./faucet.script";

export function FaucetUi(props: FaucetState) {
  const { t } = useTranslation();

  if (!props.showFaucet) {
    return null;
  }

  return (
    <Button
      variant="outlined"
      fullWidth
      size="md"
      onClick={props.getFaucet}
      loading={props.loading}
      className="oui-faucet-btn oui-rounded oui-border-primary oui-text-primary"
      data-testid="oui-testid-assetView-getFaucet-button"
    >
      {t("trading.faucet.getTestUSDC")}
    </Button>
  );
}
