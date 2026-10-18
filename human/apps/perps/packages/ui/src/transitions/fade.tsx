import { Slot } from "@radix-ui/react-slot";
import { FC, PropsWithChildren } from "react";
import { cnBase } from "tailwind-variants";

export interface FadeProps extends React.HTMLAttributes<HTMLDivElement> {
  in: boolean;
  asChild?: boolean;
}

export const Fade: FC<PropsWithChildren<FadeProps>> = (props) => {
  const { in: inProp, className, asChild = false, ...rest } = props;
  const Comp = asChild ? Slot : "div";
  return (
    <Comp
      className={cnBase("oui-animate oui-fade-in oui-fade-out", className)}
      {...rest}
    />
  );
};
