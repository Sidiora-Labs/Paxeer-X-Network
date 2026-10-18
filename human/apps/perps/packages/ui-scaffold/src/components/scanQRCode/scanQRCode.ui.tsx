import { FC, SVGProps } from "react";
import { useTranslation, Trans } from "@paxeer-network/i18n";
import {
  Box,
  cn,
  Flex,
  SimpleDialog,
  Text,
  Tooltip,
} from "@paxeer-network/ui";
import { MainLogo } from "../main/mainLogo";
import { UseScanQRCodeScriptReturn } from "./scanQRCode.script";
import { QRCodeScanner, QRCODE_WIDTH, QRCODE_HEIGHT } from "./scanner";

type ScanQRCodeProps = UseScanQRCodeScriptReturn;

export const ScanQRCode: FC<ScanQRCodeProps> = (props) => {
  const { t } = useTranslation();
  return (
    <>
      <Tooltip
        open={props.showScanTooltip}
        content={t("linkDevice.scanQRCode.tooltip")}
        className="oui-bg-base-6 oui-text-2xs oui-font-semibold oui-text-warning-darken"
        arrow={{ className: "!oui-fill-base-6" }}
      >
        <Flex
          className="oui-h-8 oui-w-8 oui-cursor-pointer oui-items-center oui-justify-center oui-rounded-md oui-bg-base-5 oui-px-[6px]"
          onClick={props.showDialog}
        >
          <ScanIcon className="oui-text-base-contrast-80" />
        </Flex>
      </Tooltip>
      <SimpleDialog
        title={<MainLogo />}
        open={props.open}
        onOpenChange={props.onOpenChange}
        size="sm"
      >
        <ScanQRCodeContent {...props} />
      </SimpleDialog>
    </>
  );
};

const ScanQRCodeContent: FC<ScanQRCodeProps> = (props) => {
  const { t } = useTranslation();

  return (
    <Flex justify="center" direction="column" gapY={5}>
      <Box width={QRCODE_WIDTH} height={QRCODE_HEIGHT} className="oui-relative">
        <QRCodeScanner onSuccess={props.onScanSuccess} />
        <>
          <LineGradient />
          <RadiusGradient className="oui-absolute oui-start-[-1.5px] oui-top-[-1.5px]" />
          <RadiusGradient className="oui-absolute oui-end-[-1.5px] oui-top-[-1.5px] oui-rotate-90" />
          <RadiusGradient className="oui-absolute oui-bottom-[-1.5px] oui-start-[-1.5px] -oui-rotate-90" />
          <RadiusGradient className="oui-absolute oui-bottom-[-1.5px] oui-end-[-1.5px] -oui-rotate-180" />
        </>
      </Box>

      {/* </Box> */}
      <Text size="sm" intensity={98} weight="semibold">
        {t("linkDevice.scanQRCode")}
      </Text>
      <span className="oui-px-3 oui-text-center">
        <Text
          size="2xs"
          intensity={54}
          weight="regular"
          className="oui-break-words"
        >
          {/* @ts-ignore */}
          <Trans
            i18nKey="linkDevice.scanQRCode.description"
            components={[
              <LinkDeviceIcon
                key="0"
                className="oui-mx-1 oui-inline-block oui-text-base-contrast-80"
              />,
            ]}
          />
        </Text>
      </span>
    </Flex>
  );
};

const ScanIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="18"
    height="18"
    viewBox="0 0 18 18"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <path d="M5.249 2.243a3 3 0 0 0-3 3v.75a.75.75 0 0 0 1.5 0v-.75a1.5 1.5 0 0 1 1.5-1.5h2.25a.75.75 0 0 0 0-1.5zm5.25 0a.75.75 0 0 0 0 1.5h2.25a1.5 1.5 0 0 1 1.5 1.5v.75a.75.75 0 0 0 1.5 0v-.75a3 3 0 0 0-3-3zm-7.5 9a.75.75 0 0 0-.75.75v.75a3 3 0 0 0 3 3h2.25a.75.75 0 0 0 0-1.5h-2.25a1.5 1.5 0 0 1-1.5-1.5v-.75a.75.75 0 0 0-.75-.75m12 0a.75.75 0 0 0-.75.75v.75a1.5 1.5 0 0 1-1.5 1.5h-2.25a.75.75 0 0 0 0 1.5h2.25a3 3 0 0 0 3-3v-.75a.75.75 0 0 0-.75-.75M5.25 8.999a.75.75 0 0 1 .75-.75h6a.75.75 0 0 1 0 1.5H6a.75.75 0 0 1-.75-.75" />
  </svg>
);

export interface LinkDeviceIconProps extends SVGProps<SVGSVGElement> {
  size?: number;
}

export const LinkDeviceIcon: FC<LinkDeviceIconProps> = (props) => {
  const { size = 20, ...rest } = props;
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 20 20"
      fill="currentColor"
      xmlns="http://www.w3.org/2000/svg"
      {...rest}
    >
      <path d="M19.167 7.583a1.74 1.74 0 0 0-1.731-1.75h-4.038a1.74 1.74 0 0 0-1.731 1.75v8.167c0 .967.775 1.75 1.73 1.75h4.039a1.74 1.74 0 0 0 1.73-1.75zm-1.154 0v7.584H12.82V7.583A.58.58 0 0 1 13.398 7h4.038a.58.58 0 0 1 .577.583m-2.02 8.75a.58.58 0 0 1-.576.584.58.58 0 0 1-.577-.584.58.58 0 0 1 .577-.583.58.58 0 0 1 .577.583" />
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        d="M1.666 5a2.5 2.5 0 0 1 2.5-2.5h10a2.5 2.5 0 0 1 2.5 2.5.08.08 0 0 1-.078.078h-1.51a.08.08 0 0 1-.08-.078.834.834 0 0 0-.833-.833h-10A.834.834 0 0 0 3.333 5v5.633c0 .11.09.2.2.2h7.1c.11 0 .2.09.2.2V12.3a.2.2 0 0 1-.2.2H2.7a.2.2 0 0 0-.2.2v.633c0 .511.308.834.834.834h7.3c.11 0 .2.09.2.2v1.266a.2.2 0 0 1-.2.2h-7.3c-1.465 0-2.5-1.086-2.5-2.5v-1.666c0-.392.27-.72.635-.81.107-.026.198-.113.198-.224z"
      />
    </svg>
  );
};

const LineGradient: FC<SVGProps<SVGSVGElement>> = () => {
  return (
    <div
      className={cn(
        "oui-absolute oui-start-[30px] oui-top-0",
        "oui-h-[2px] oui-w-[calc(100%-60px)] oui-rounded-full",
        "oui-bg-[linear-gradient(270deg,rgb(var(--oui-gradient-brand-end))_0%,rgb(var(--oui-gradient-brand-start))_100%)]",
        "oui-shadow-[0_0_8px_4px_rgba(var(--oui-gradient-brand-start)/0.12)]",
        "oui-animate-scan-qr-code",
      )}
    />
  );
};

const RadiusGradient: FC<SVGProps<SVGSVGElement>> = (props) => {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="64"
      height="64"
      viewBox="0 0 64 64"
      fill="none"
      {...props}
    >
      <path
        d="M62 2H18C9.16344 2 2 9.16344 2 18V62"
        stroke="url(#paint0_linear_136_6227)"
        strokeWidth="3"
        strokeLinecap="round"
      />
      <defs>
        <linearGradient
          id="paint0_linear_136_6227"
          x1="2"
          y1="32"
          x2="62"
          y2="32"
          gradientUnits="userSpaceOnUse"
        >
          <stop stopColor="rgb(var(--oui-gradient-brand-end))" />
          <stop offset="1" stopColor="rgb(var(--oui-gradient-brand-start))" />
        </linearGradient>
      </defs>
    </svg>
  );
};
