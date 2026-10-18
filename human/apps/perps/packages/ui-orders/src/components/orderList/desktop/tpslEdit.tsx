import { useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { API } from "@paxeer-network/types";
import { Button } from "@paxeer-network/ui";
import { TPSLEditModal } from "@paxeer-network/ui-tpsl";
import { useTPSLOrderRowContext } from "../tpslOrderRowContext";

export const TP_SLEditButton = (_props: { order: API.Order }) => {
  const { position, order } = useTPSLOrderRowContext();
  const { t } = useTranslation();
  const [mounted, setMounted] = useState(false);
  const [open, setOpen] = useState(false);

  return (
    <>
      <Button
        variant="outlined"
        size="sm"
        color="secondary"
        onClick={() => {
          setMounted(true);
          setOpen(true);
        }}
      >
        {t("common.edit")}
      </Button>
      {mounted && (
        <TPSLEditModal
          mode="dialog"
          open={open}
          onOpenChange={setOpen}
          order={order}
          position={position}
        />
      )}
    </>
  );
};
