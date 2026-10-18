import { useRef } from "react";
import { useBoolean } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import {
  EditIcon,
  Flex,
  Text,
  SimpleDialog,
  useScreen,
} from "@paxeer-network/ui";
import { AuthGuard } from "@paxeer-network/ui-connector";
import { SlippageEditor } from "./slippageEditor";

export const SlippageCell = (props: {
  slippage: string;
  setSlippage: (slippage: string) => void;
  estSlippage: number | null;
}) => {
  const { t } = useTranslation();
  const [open, { setTrue: setOpen, setFalse: setClose, toggle }] =
    useBoolean(false);

  const { isMobile } = useScreen();
  const slippageRef = useRef<{ getValue: () => number | undefined }>(null);

  const onConfirm = () => {
    const val = slippageRef.current?.getValue();

    props.setSlippage(!val ? "1" : val.toString());
    setClose();
    return Promise.resolve(true);
  };

  return (
    <>
      <SimpleDialog
        open={open}
        onOpenChange={toggle}
        title={t("common.settings")}
        contentProps={{ size: isMobile ? "xs" : "sm" }}
        classNames={{
          footer: "oui-orderEntry-slippage-footer",
          body: "oui-orderEntry-slippage-body",
        }}
        actions={{
          primary: {
            disabled: false,
            label: t("common.save"),
            onClick: onConfirm,
            className: "oui-slippage-save-btn",
          },
          secondary: {
            label: t("common.cancel"),
            onClick: () => setClose(),
            className: "oui-slippage-cancel-btn",
          },
        }}
      >
        <SlippageEditor
          ref={slippageRef}
          isMobile={isMobile}
          initialValue={props.slippage ? Number(props.slippage) : undefined}
        />
      </SimpleDialog>
      <Flex
        justify={"between"}
        itemAlign="center"
        gap={1}
        className="oui-orderEntry-slippage oui-w-full"
      >
        <Text
          className="oui-slippage-label oui-shrink-0 oui-whitespace-nowrap"
          size="2xs"
        >
          {t("orderEntry.slippage")}
        </Text>
        <AuthGuard
          fallback={() => (
            <Text size="2xs" className="oui-whitespace-nowrap">
              {t("orderEntry.slippage.est")}: -% / {t("common.max")}: --%
            </Text>
          )}
        >
          <Flex
            gap={1}
            itemAlign="center"
            justify="end"
            className="oui-slippage-value-container oui-min-w-0 oui-flex-1 oui-whitespace-nowrap"
          >
            <Text.numeral
              size="2xs"
              rule="percentages"
              prefix={`${t("orderEntry.slippage.est")}: `}
              suffix={` / ${t("common.max")}: `}
              className="oui-whitespace-nowrap"
            >
              {props.estSlippage ?? 0}
            </Text.numeral>
            <button
              className="oui-slippage-edit-btn oui-shrink-0 oui-text-2xs"
              onClick={() => setOpen()}
            >
              <Flex
                itemAlign="center"
                className="oui-gap-0.5 oui-whitespace-nowrap"
                as="span"
              >
                <Text size="2xs" className="oui-text-primary">
                  {`${props.slippage || "-"}%`}
                </Text>
                <EditIcon
                  className="oui-slippage-edit-icon oui-text-primary oui-hidden md:oui-block"
                  size={12}
                  opacity={1}
                />
              </Flex>
            </button>
          </Flex>
        </AuthGuard>
      </Flex>
    </>
  );
};
