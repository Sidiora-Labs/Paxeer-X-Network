import { FC } from "react";
import { Button, formatAddress } from "@paxeer-network/ui";
import { AuthGuard } from "@paxeer-network/ui-connector";
import { AccountState } from "./account.script";

export const Account: FC<AccountState> = (props) => {
  return (
    <AuthGuard
      buttonProps={{
        size: "sm",
      }}
    >
      <Button
        variant="contained"
        size={"sm"}
        className="oui-max-w-[83px]"
        onClick={(e) => {
          props.onShowAccountSheet();
        }}
      >
        {formatAddress(props.address!, [4, 4])}
      </Button>
    </AuthGuard>
  );
};
