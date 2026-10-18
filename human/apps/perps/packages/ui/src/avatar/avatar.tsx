import * as React from "react";
import { useMemo } from "react";
import * as AvatarPrimitive from "@radix-ui/react-avatar";
import makeBlockie from "ethereum-blockies-base64";
import { type VariantProps } from "tailwind-variants";
import { tv } from "../utils/tv";

const avatarVariants = tv({
  slots: {
    root: "oui-relative oui-flex oui-shrink-0 oui-overflow-hidden oui-rounded-full",
    image: "oui-aspect-square oui-h-full oui-w-full",
    fallback:
      "oui-flex oui-h-full oui-w-full oui-items-center oui-justify-center oui-rounded-full oui-bg-base-2",
  },
  variants: {
    size: {
      "3xs": {
        root: "oui-w-3.5 oui-h-3.5",
      },
      "2xs": {
        root: "oui-w-4 oui-h-4",
      },
      xs: {
        root: "oui-w-5 oui-h-5",
      },
      sm: {
        root: "oui-w-6 oui-h-6",
      },
      md: {
        root: "oui-w-8 oui-h-8",
      },
      lg: {
        root: "oui-w-10 oui-h-10",
      },
      xl: {
        root: "oui-w-12 oui-h-12",
      },
    },
  },
  defaultVariants: {
    size: "sm",
  },
});

export type AvatarSizeType = VariantProps<typeof avatarVariants>["size"];

const AvatarBase = React.forwardRef<
  React.ElementRef<typeof AvatarPrimitive.Root>,
  React.ComponentPropsWithoutRef<typeof AvatarPrimitive.Root> &
    VariantProps<typeof avatarVariants>
>(({ className, size, ...props }, ref) => {
  const { root } = avatarVariants({ size });
  return (
    <AvatarPrimitive.Root
      ref={ref}
      {...props}
      className={root({ className })}
    />
  );
});
AvatarBase.displayName = AvatarPrimitive.Root.displayName;

const AvatarImage = React.forwardRef<
  React.ElementRef<typeof AvatarPrimitive.Image>,
  React.ComponentPropsWithoutRef<typeof AvatarPrimitive.Image>
>(({ className, ...props }, ref) => {
  const { image } = avatarVariants();
  return (
    <AvatarPrimitive.Image
      ref={ref}
      className={image({ className })}
      {...props}
    />
  );
});
AvatarImage.displayName = AvatarPrimitive.Image.displayName;

const AvatarFallback = React.forwardRef<
  React.ElementRef<typeof AvatarPrimitive.Fallback>,
  React.ComponentPropsWithoutRef<typeof AvatarPrimitive.Fallback>
>(({ className, ...props }, ref) => {
  const { fallback } = avatarVariants({ className });
  return (
    <AvatarPrimitive.Fallback ref={ref} className={fallback()} {...props} />
  );
});
AvatarFallback.displayName = AvatarPrimitive.Fallback.displayName;

type AvatarProps = React.ComponentProps<typeof AvatarBase> &
  VariantProps<typeof avatarVariants> & {
    src?: string;
    alt?: string;
    fallback?: React.ReactNode;
    delayMs?: number;
    onLoadingStatusChange?: AvatarPrimitive.AvatarImageProps["onLoadingStatusChange"];
  };

const Avatar = React.forwardRef<
  React.ElementRef<typeof AvatarPrimitive.Root>,
  AvatarProps
>((props, ref) => {
  const { size, src, fallback, delayMs, alt, onLoadingStatusChange, ...rest } =
    props;
  return (
    <AvatarBase {...rest} ref={ref} size={size}>
      <AvatarImage
        src={src}
        onLoadingStatusChange={onLoadingStatusChange}
        alt={alt}
      />
      {typeof fallback !== "undefined" && (
        <AvatarFallback delayMs={delayMs}>{fallback}</AvatarFallback>
      )}
    </AvatarBase>
  );
});

const EVMAvatar = React.forwardRef<
  React.ElementRef<typeof Avatar>,
  AvatarProps & {
    address: string;
  }
>((props, ref) => {
  const { address, ...rest } = props;
  const src = useMemo(() => makeBlockie(address), [props.address]);
  return <Avatar {...rest} src={src} />;
});

export {
  AvatarBase,
  AvatarImage,
  AvatarFallback,
  Avatar,
  EVMAvatar,
  avatarVariants,
};
