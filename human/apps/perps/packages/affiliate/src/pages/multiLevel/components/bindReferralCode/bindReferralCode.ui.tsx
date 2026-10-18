import { REFERRAL_CODE_MIN_LENGTH } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { Button, Checkbox, Divider, Flex, Text } from "@paxeer-network/ui";
import { ReferralCodeInput } from "../../affiliate/referralCodeForm/referralCodeInput";
import { WarningBox } from "../warningBox";
import { BindReferralCodeScriptReturns } from "./bindReferralCode.script";
import { BindReferralCodeWidgetProps } from "./bindReferralCode.widget";

export type BindReferralCodeProps = BindReferralCodeScriptReturns &
  BindReferralCodeWidgetProps;

export const BindReferralCode = (props: BindReferralCodeProps) => {
  const { t } = useTranslation();

  const bindCodeInvalid =
    !props.skipBinding &&
    props.formattedBindCode.length >= REFERRAL_CODE_MIN_LENGTH &&
    !props.isBindCodeChecking &&
    props.isBindCodeExist === false;

  const titleView = (
    <Flex
      width={"100%"}
      direction="column"
      itemAlign="start"
      gap={3}
      className="oui-bindReferralCode-header"
    >
      <Text size="base" intensity={98}>
        {t("affiliate.referralCode.bind.modal.title")}
      </Text>
      <Divider intensity={8} className="oui-w-full" />
    </Flex>
  );

  const descriptionView = (
    <WarningBox>
      <Text
        size="2xs"
        intensity={54}
        className="oui-leading-[18px] oui-text-warning-darken"
      >
        {t("affiliate.referralCode.bind.modal.description")}
      </Text>
    </WarningBox>
  );

  return (
    <Flex
      direction="column"
      itemAlign="start"
      gap={6}
      className="oui-affiliate-bindReferralCode oui-font-semibold"
    >
      {titleView}
      {descriptionView}

      <Flex width={"100%"} direction="column" itemAlign="start" gap={4}>
        <ReferralCodeInput
          value={props.bindCodeInput}
          onChange={props.setBindCodeInput}
          autoFocus
          disabled={props.skipBinding}
          placeholder={t("affiliate.referralCode.bind.input.placeholder")}
          helpText={
            bindCodeInvalid ? t("affiliate.referralCode.notExist") : undefined
          }
          color={bindCodeInvalid ? "danger" : undefined}
        />

        <Flex className="oui-gap-[6px]">
          <Checkbox
            color="white"
            id="oui-checkbox-bindReferralCode-skip"
            checked={props.skipBinding}
            onCheckedChange={(checked: boolean) => {
              props.setSkipBinding(checked);
              if (checked) {
                props.setBindCodeInput("");
              }
            }}
          />
          <label
            htmlFor="oui-checkbox-bindReferralCode-skip"
            className="oui-text-2xs oui-font-normal oui-text-base-contrast-54"
          >
            {t("affiliate.referralCode.bind.skip")}
          </label>
        </Flex>

        <Flex direction={"row"} gap={2} width={"100%"} mt={0} pt={5}>
          <Button
            variant="contained"
            color="gray"
            fullWidth
            onClick={props.close}
            size="md"
            className="oui-bindReferralCode-cancel-btn"
          >
            {t("common.cancel")}
          </Button>
          <Button
            fullWidth
            onClick={props.onConfirm}
            disabled={props.buttonDisabled || props.confirmButtonLoading}
            loading={props.confirmButtonLoading}
            size="md"
            className="oui-bindReferralCode-confirm-btn"
          >
            {t("common.confirm")}
          </Button>
        </Flex>
      </Flex>
    </Flex>
  );
};
