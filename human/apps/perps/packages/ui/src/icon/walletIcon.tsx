import { useMemo, type FC } from "react";
import { Avatar, AvatarBase, avatarVariants } from "../avatar/avatar";
import type { VariantProps } from "tailwind-variants";

export type WalletIconProps = {
  name: string;
} & Pick<VariantProps<typeof avatarVariants>, "size">;

export const WalletIcon: FC<WalletIconProps> = (props) => {
  const url = useMemo(() => {
    const split = props.name?.split(" ");
    const formatWalletName = split?.[0]?.toLowerCase();
    return `/assets/static/wallet_icon/${formatWalletName}.png`;
  }, [props.name]);
  return <Avatar size={props.size} src={url} alt={`${props.name}`} />;
};
