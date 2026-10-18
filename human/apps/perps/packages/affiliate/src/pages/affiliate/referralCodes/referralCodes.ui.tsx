import { FC, ReactNode, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  Button,
  DataTable,
  Divider,
  Flex,
  ListView,
  Statistic,
  Text,
  cn,
  Column,
  CopyIcon,
  useScreen,
} from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { EditCode } from "../../../components/editCodeBtn";
import { PinBtn } from "../../../components/pinButton";
import { EditIcon } from "../../../icons/editIcon";
import { ReferralCodesReturns, ReferralCodeType } from "./referralCodes.script";

export const ReferralCodes: FC<ReferralCodesReturns> = (props) => {
  const { isMobile } = useScreen();
  return (
    <Flex
      r={"2xl"}
      p={6}
      width={"100%"}
      gap={4}
      direction={"column"}
      className="oui-h-full oui-bg-base-9 oui-p-6 oui-tabular-nums"
    >
      <Title {...props} />

      <div className="oui-flex oui-w-full oui-flex-col 2xl:oui-h-full">
        <Divider />
        {isMobile ? <MobileLayout {...props} /> : <DesktopLayout {...props} />}
      </div>
    </Flex>
  );
};

const Title: FC<ReferralCodesReturns> = (props) => {
  const { t } = useTranslation();

  return (
    <Flex direction={"row"} justify={"between"} width={"100%"}>
      <Text className="oui-text-lg">{t("affiliate.referralCodes")}</Text>
      <div className="oui-text-2xs md:oui-text-xs xl:oui-text-sm">
        <Text className="oui-text-base-contrast-54">
          {`${t("affiliate.referralCodes.remaining")}: `}
        </Text>
        <Text className="oui-text-primary-light">
          {props.codes?.length || "--"}
        </Text>
      </div>
    </Flex>
  );
};

const MobileLayout: FC<ReferralCodesReturns> = (props) => {
  return (
    <ListView
      dataSource={props.codes}
      keyExtractor={(item) => item.code}
      className="oui-max-h-[240px] oui-w-full"
      renderItem={(e) => {
        return (
          <Flex direction={"column"}>
            <MobileCell
              data={e}
              editRate={props.editRate}
              copyLink={props.copyLink}
              copyCode={props.copyCode}
              setPinCode={props.setPinCode}
              editCode={props.editCode}
            />
            <Divider className="oui-mt-3 oui-w-full" />
          </Flex>
        );
      }}
    />
  );
};

const MobileCellItem: FC<{
  // key: string;
  title: string;
  value: string | ReactNode;
  copyable?: boolean;
  align?: "start" | "end" | undefined;
  className?: string;
  editRate?: () => void;
  onCopy?: () => void;
}> = (props) => {
  const { title, copyable, value, align, className, editRate, onCopy } = props;
  return (
    <Statistic
      id="oui-affiliate-affiliate-referralCodes"
      className={cn("oui-flex-1", className)}
      label={
        <Text className="oui-text-2xs oui-text-base-contrast-36">{title}</Text>
      }
      align={align}
      children={
        <Flex direction={"row"} gap={1}>
          <Text.formatted
            copyable={copyable}
            onCopy={() => {
              onCopy?.();
            }}
            className="oui-mt-[6px] oui-text-sm oui-text-base-contrast-80"
          >
            {value as string}
          </Text.formatted>
          {editRate && (
            <EditIcon
              className="oui-mt-px oui-cursor-pointer oui-fill-base-contrast-36 hover:oui-fill-base-contrast-80"
              fillOpacity={1}
              fill="currentColor"
              onClick={() => editRate()}
            />
          )}
        </Flex>
      }
    />
  );
};
const MobileCell: FC<{
  data: ReferralCodeType;
  setPinCode: (code: string, del?: boolean) => void;
  copyLink: (code: string) => void;
  copyCode: (code: string) => void;
  editRate: (code: ReferralCodeType) => void;
  editCode: (code: ReferralCodeType) => void;
}> = (props) => {
  const { data, setPinCode, copyLink, editRate, editCode } = props;
  const { t } = useTranslation();

  return (
    <Flex key={data.code} gap={3} direction={"column"} className="oui-w-full">
      <Flex
        direction={"row"}
        justify={"between"}
        itemAlign={"stretch"}
        width={"100%"}
      >
        <MobileCellItem
          title={t("affiliate.referralCode")}
          value={data.code}
          copyable
          onCopy={() => {
            props.copyCode?.(data.code);
          }}
          editRate={() => {
            editCode(data);
          }}
        />
        <MobileCellItem
          title={t("affiliate.referralCodes.column.you&Referee")}
          value={getRate(data)}
          align="end"
          editRate={() => {
            editRate(data);
          }}
        />
        <MobileCellItem
          title={t("affiliate.referralCodes.column.referees&Traders")}
          value={getCount(data)}
          align="end"
          className={"oui-hidden md:oui-flex"}
        />
      </Flex>
      <Flex
        direction={"row"}
        justify={"between"}
        itemAlign={"stretch"}
        width={"100%"}
        className="md:oui-hidden"
      >
        <MobileCellItem
          title={t("affiliate.referees")}
          value={getCount(data).split("/")?.[0]}
          align="start"
        />
        <MobileCellItem
          title={t("affiliate.referralCodes.column.traders")}
          value={getCount(data).split("/")?.[1]}
          align="end"
        />
      </Flex>
      <Flex
        direction={"row"}
        justify={"between"}
        itemAlign={"stretch"}
        width={"100%"}
      >
        <PinBtn
          pinned={data.isPined || false}
          onClick={(e) => {
            setPinCode(data.code, !e);
          }}
        />
        <Button
          variant="outlined"
          size="xs"
          className="oui-px-[20px]"
          onClick={(e) => {
            copyLink(data.code);
          }}
        >
          {t("affiliate.referralCodes.copyLink")}
        </Button>
      </Flex>
    </Flex>
  );
};

const DesktopLayout: FC<ReferralCodesReturns> = (props) => {
  const { t } = useTranslation();

  const { isDesktop: moreColumn } = useScreen();

  const columns = useMemo(() => {
    const cols: Column[] = [
      {
        title: t("affiliate.referralCode"),
        dataIndex: "code",
        width: moreColumn ? 115 : 120,
        className: "!oui-px-0",
        render: (value, data) => {
          return (
            <Flex direction={"row"} itemAlign={"center"} gap={1}>
              <PinBtn
                size={12}
                pinned={data.isPined || false}
                onClick={(e) => {
                  props.setPinCode(data.code, !e);
                }}
              />
              <Text.formatted
                // rule={""}
                suffix={
                  <>
                    {data.isAutoGenerated && data.total_invites < 1 && (
                      <EditCode onClick={() => props.editCode?.(data)} />
                    )}
                    <CopyIcon
                      className="oui-cursor-pointer"
                      size={12}
                      color="white"
                      onClick={() => props.copyCode?.(data.code)}
                    />
                  </>
                }
                copyable
                onCopy={() => {
                  props.copyCode?.(data.code);
                }}
              >
                {value}
              </Text.formatted>
            </Flex>
          );
        },
      },
      {
        title: t("affiliate.referralCodes.column.you&Referee"),
        dataIndex: "dffd",
        width: moreColumn ? 120 : 120,
        className: "oui-pe-0",
        render: (value, data) => {
          return (
            <Flex direction={"row"} itemAlign={"center"} gap={1}>
              {getRate(data)}
              <EditIcon
                className="oui-mt-[6px] oui-cursor-pointer oui-fill-base-contrast-36 hover:oui-fill-base-contrast-80"
                fillOpacity={1}
                fill="currentColor"
                onClick={(e) => props.editRate?.(data)}
              />
            </Flex>
          );
        },
      },
    ];

    if (moreColumn) {
      cols.push({
        title: t("affiliate.referees"),
        dataIndex: "referee_rebate_rate",
        width: 65,
        className: "oui-pe-0",
        render: (value, data) => getCount(data).split("/")[0],
      });
      cols.push({
        title: t("affiliate.referralCodes.column.traders"),
        dataIndex: "referrer_rebate_rate",
        width: 65,
        className: "oui-pe-0",
        render: (value, data) => getCount(data).split("/")[1],
      });
    } else {
      cols.push({
        title: t("affiliate.referralCodes.column.referees&Traders"),
        dataIndex: "total_invites/total_traded",
        width: 120,
        fixed: "start",
        render: (value, data) => getCount(data),
      });
    }

    cols.push({
      dataIndex: "link",
      align: "right",
      width: 74,
      className: "!oui-px-0",
      render: (value, data) => (
        <Button
          variant="outlined"
          size="sm"
          className="oui-px-5"
          onClick={(e) => {
            props?.copyLink?.(data.code);
          }}
        >
          {t("affiliate.referralCodes.copyLink")}
        </Button>
      ),
    });

    return cols;
  }, [moreColumn, t]);

  return (
    <DataTable
      bordered
      columns={columns}
      dataSource={props.codes}
      generatedRowKey={(record: ReferralCodeType) => record.code}
      classNames={{
        header: "oui-px-0",
        root: "2xl:oui-flex-1 2xl:oui-max-h-[230px] 3xl:oui-max-h-[300px]",
      }}
      onRow={(record) => {
        return {
          className: "oui-h-[45px]",
        };
      }}
    />
  );
};

const getRate = (item: ReferralCodeType) => {
  const refereeRate = new Decimal(item.referee_rebate_rate)
    .mul(100)
    .toFixed(1, Decimal.ROUND_DOWN)
    .toString();
  const referralRate = new Decimal(item.referrer_rebate_rate)
    .mul(100)
    .toFixed(1, Decimal.ROUND_DOWN)
    .toString();
  return `${referralRate}% / ${refereeRate}%`;
};

const getCount = (item: ReferralCodeType) => {
  return `${item.total_invites} / ${item.total_traded}`;
};
