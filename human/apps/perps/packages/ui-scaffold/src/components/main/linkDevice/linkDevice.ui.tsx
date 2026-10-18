import { FC, SVGProps, forwardRef, useEffect, useRef } from "react";
import { qrcode as qr } from "@akamfoad/qr";
import { Trans, useTranslation } from "@paxeer-network/i18n";
import {
  cn,
  CopyIcon,
  Flex,
  SimpleDialog,
  SimpleDialogFooter,
  SimpleDialogFooterProps,
  Text,
  Tooltip,
} from "@paxeer-network/ui";
import { MainLogo } from "../mainLogo";
import { UseLinkDeviceScriptReturn } from "./linkDevice.script";

export type LinkDeviceProps = UseLinkDeviceScriptReturn;

export const LinkDevice: FC<LinkDeviceProps> = (props) => {
  const { t } = useTranslation();

  return (
    <>
      <Tooltip content={t("linkDevice.tooltip")}>
        <LinkDeviceIcon
          className="oui-text-base-contrast-80 oui-cursor-pointer hover:oui-fill-base-contrast oui-transition-colors"
          onClick={props.showDialog}
        />
      </Tooltip>

      <SimpleDialog
        title={<Text weight="semibold">{t("common.confirm")}</Text>}
        open={props.open}
        onOpenChange={props.onOpenChange}
        size="sm"
        contentProps={{
          onInteractOutside: (e) => {
            const el = document.querySelector("#privy-dialog");
            if (el) {
              e.preventDefault();
            }
          },
        }}
      >
        <LinkDeviceContent {...props} />
      </SimpleDialog>
    </>
  );
};

export const LinkDeviceContent: FC<LinkDeviceProps> = (props) => {
  if (props.loading) {
    return <Loading />;
  }

  if (props.confirm) {
    return (
      <QRCode
        hideDialog={props.hideDialog}
        seconds={props.seconds}
        url={props.url}
        copyUrl={props.copyUrl}
      />
    );
  }

  return (
    <LinkDeviceConfirm
      hideDialog={props.hideDialog}
      onConfirm={props.onConfirm}
    />
  );
};

type QRCodeProps = Pick<
  LinkDeviceProps,
  "seconds" | "hideDialog" | "copyUrl"
> & {
  url?: string;
};

const QRCode: FC<QRCodeProps> = (props) => {
  const { t } = useTranslation();
  const actions: SimpleDialogFooterProps["actions"] = {
    primary: {
      label: t("common.ok"),
      onClick: props.hideDialog,
      size: "md",
    },
  };

  return (
    <Flex direction="column" gapY={3}>
      <Text size="base" intensity={98}>
        {t("linkDevice.scanQRCode")}
      </Text>
      <Text
        size="2xs"
        intensity={54}
        weight="regular"
        className="oui-text-center"
      >
        {/* @ts-ignore */}
        <Trans i18nKey="linkDevice.createQRCode.success.description" />
      </Text>

      <Text size="sm" intensity={54}>
        {`${t("common.countdown")}: `}
        <Text.gradient color="brand" className="oui-tabular-nums">
          {props.seconds}s
        </Text.gradient>
      </Text>

      <Flex
        className={cn(
          "oui-w-[276px] oui-h-[276px] ",
          "oui-border oui-border-base-contrast-20 oui-rounded-2xl",
        )}
        justify="center"
        itemAlign="center"
      >
        <Flex
          className="oui-w-[256px] oui-h-[256px] oui-rounded-lg oui-bg-base-contrast"
          justify="center"
          itemAlign="center"
        >
          <QRCodeCanvas width={232} height={232} content={props.url} />
        </Flex>
      </Flex>

      <Flex
        direction="row"
        gap={1}
        className={cn(
          "oui-cursor-pointer",
          "oui-group oui-text-base-contrast-54 hover:oui-text-base-contrast",
        )}
        onClick={props.copyUrl}
      >
        <CopyIcon
          size={16}
          opacity={1}
          className="oui-text-base-contrast-54 group-hover:oui-text-base-contrast"
        />
        <Text size="2xs" weight="regular">
          {t("linkDevice.createQRCode.success.copyUrl")}
        </Text>
      </Flex>

      <SimpleDialogFooter
        actions={actions}
        className="oui-w-full oui-p-0 !oui-pt-8"
      />
    </Flex>
  );
};

type QRCodeCanvasProps = {
  width: number;
  height: number;
  content?: string;
};

const QRCodeCanvas: FC<QRCodeCanvasProps> = (props) => {
  const canvasRef = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    if (!canvasRef.current || !props.content) return;

    const qrcode = qr(props.content);
    const width = props.width;
    const height = props.height;

    const ctx = canvasRef.current.getContext("2d")!;

    const cells = qrcode.modules!;

    const tileW = width / cells.length;
    const tileH = height / cells.length;

    for (let r = 0; r < cells.length; ++r) {
      const row = cells[r];
      for (let c = 0; c < row.length; ++c) {
        ctx.fillStyle = row[c] ? "#000" : "#fff";
        const w = Math.ceil((c + 1) * tileW) - Math.floor(c * tileW);
        const h = Math.ceil((r + 1) * tileH) - Math.floor(r * tileH);
        ctx.fillRect(Math.round(c * tileW), Math.round(r * tileH), w, h);
      }
    }
  }, [canvasRef, props.content]);

  return <canvas width={props.width} height={props.height} ref={canvasRef} />;
};

type LinkDeviceConfirmProps = Pick<LinkDeviceProps, "hideDialog" | "onConfirm">;

const LinkDeviceConfirm: FC<LinkDeviceConfirmProps> = (props) => {
  const { t } = useTranslation();

  const actions: SimpleDialogFooterProps["actions"] = {
    secondary: {
      label: t("common.cancel"),
      onClick: props.hideDialog,
      className: "oui-flex-1",
      size: "md",
    },
    primary: {
      label: t("common.confirm"),
      onClick: props.onConfirm,
      className: "oui-flex-1",
      size: "md",
    },
  };

  return (
    <Flex direction="column">
      <MainLogo />
      <Text size="base" intensity={98} className="oui-mt-5">
        {t("linkDevice.createQRCode.linkMobileDevice")}
      </Text>
      <Text
        size="2xs"
        intensity={54}
        weight="regular"
        className="oui-text-center oui-mt-3"
      >
        {/* @ts-ignore */}
        <Trans
          i18nKey="linkDevice.createQRCode.linkMobileDevice.description"
          values={{
            hostname: window.location.hostname,
          }}
        />
      </Text>
      <SimpleDialogFooter
        actions={actions}
        className="oui-w-full oui-p-0 !oui-pt-8"
      />
    </Flex>
  );
};

const Loading = () => {
  const { t } = useTranslation();

  return (
    <Flex direction="column" gap={5}>
      <Spinner />
      <Text size="sm" intensity={98}>
        {t("linkDevice.createQRCode.loading.description")}
      </Text>
    </Flex>
  );
};

const Spinner = () => {
  return (
    <svg
      width="80"
      height="80"
      viewBox="0 0 80 80"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className="oui-animate-spin"
    >
      <path
        d="M11.4858 52.1631C10.4698 52.5965 9.28872 52.1259 8.91766 51.0855C7.68926 47.6412 7.04029 44.0121 7.00182 40.3463C6.95634 36.0129 7.76483 31.713 9.38113 27.6921C10.9974 23.6712 13.3899 20.0079 16.4219 16.9116C18.9868 14.2923 21.967 12.122 25.2375 10.4861C26.2253 9.99202 27.4035 10.4698 27.8369 11.4858L28.8571 13.8773C29.2904 14.8933 28.8139 16.0615 27.8336 16.5706C25.3569 17.8567 23.0959 19.5294 21.1375 21.5293C18.7119 24.0064 16.7979 26.9369 15.5049 30.1537C14.2119 33.3704 13.5651 36.8103 13.6015 40.277C13.6308 43.076 14.1051 45.8482 15.0026 48.4906C15.3579 49.5365 14.8933 50.7096 13.8773 51.143L11.4858 52.1631Z"
        fill="url(#paint0_linear_177_6754)"
      />
      <path
        d="M73 40C73 58.2254 58.2254 73 40 73C21.7746 73 7 58.2254 7 40C7 21.7746 21.7746 7 40 7C58.2254 7 73 21.7746 73 40ZM13.6 40C13.6 54.5803 25.4197 66.4 40 66.4C54.5803 66.4 66.4 54.5803 66.4 40C66.4 25.4197 54.5803 13.6 40 13.6C25.4197 13.6 13.6 25.4197 13.6 40Z"
        className="oui-fill-base-contrast-6"
      />
      <defs>
        <linearGradient
          id="paint0_linear_177_6754"
          x1="73"
          y1="40"
          x2="7"
          y2="40"
          gradientUnits="userSpaceOnUse"
        >
          <stop stopColor="rgb(var(--oui-gradient-brand-end))" />
          <stop offset="1" stopColor="rgb(var(--oui-gradient-brand-start))" />
        </linearGradient>
      </defs>
    </svg>
  );
};

export interface IconProps extends SVGProps<SVGSVGElement> {
  size?: number;
}

export const LinkDeviceIcon = forwardRef<SVGSVGElement, IconProps>(
  (props, ref) => {
    const { size = 20, viewBox, ...rest } = props;
    return (
      <svg
        ref={ref}
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
  },
);
