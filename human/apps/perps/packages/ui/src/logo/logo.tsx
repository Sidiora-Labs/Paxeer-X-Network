import React, { ElementRef, HTMLAttributes } from "react";

type Elment = ElementRef<"div">;

type LogoProps = HTMLAttributes<Elment> & {
  src: string;
  alt?: string;
  href?: string;
};

const Logo = React.forwardRef<Elment, LogoProps>(({ src, alt, href }, ref) => {
  return (
    <div className="logo oui-px-3" ref={ref}>
      <a href={href ?? "/"}>
        <img
          src={src}
          alt={alt}
          className="oui-object-contain oui-h-8 oui-py-2"
        />
      </a>
    </div>
  );
});

Logo.displayName = "LogoElement";

export { Logo };
export type { LogoProps };
