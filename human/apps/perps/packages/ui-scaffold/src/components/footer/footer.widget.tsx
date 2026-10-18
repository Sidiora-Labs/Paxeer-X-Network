import React, { ReactNode } from "react";
import { useFooterScript } from "./footer.script";
import { Footer } from "./footer.ui";

export type FooterProps = {
  telegramUrl?: string;
  twitterUrl?: string;
  discordUrl?: string;
  trailing?: ReactNode;
  poweredBy?: boolean | ReactNode;
};

export const FooterWidget: React.FC<FooterProps> = (props) => {
  const state = useFooterScript();
  return <Footer {...state} {...props} />;
};
