import React, { useEffect, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  cn,
  Divider,
  Flex,
  Grid,
  modal,
  SheetContent,
  SheetHeader,
  SimpleSheet,
  Text,
  useModal,
} from "@paxeer-network/ui";

export const PwaDialog = modal.create((props) => {
  const { visible, hide, resolve, reject, onOpenChange } = useModal();
  const [domain, setDomain] = useState("");
  const { t } = useTranslation();

  useEffect(() => {
    setDomain(window.location.hostname);
  }, []);

  return (
    <SimpleSheet open={visible} onOpenChange={onOpenChange}>
      <SheetHeader>
        <Text className="oui-text-base-contrast-80 oui-text ">
          {t("connector.privy.pwa.sheetTitle")}
        </Text>
      </SheetHeader>
      <Flex direction="column" gap={3} className="oui-w-full">
        <Text className="oui-text-base-contrast-80 oui-text ">
          {t("connector.privy.pwa.description")}
        </Text>
        <Flex direction="column" gap={4} className="oui-w-full">
          <StepItem
            index={1}
            title={t("connector.privy.pwa.step1")}
            content={
              <Flex className="oui-w-full oui-bg-dark-8">
                <Flex
                  className="oui-relative oui-h-[52px] oui-w-full oui-rounded-[44px] oui-bg-base-static-contrast/10 oui-p-2 oui-px-4"
                  itemAlign={"center"}
                  justify={"center"}
                >
                  <Text className="oui-text-base-static-contrast-80 oui-text-[18px] oui-leading-[36px] oui-flex-1 oui-text-center oui-whitespace-nowrap">
                    {domain}
                  </Text>
                  <ShareIcon className="oui-absolute oui-end-4 oui-top-1/2 oui-translate-y-[-50%]" />
                </Flex>
              </Flex>
            }
          />
          <StepItem
            index={2}
            title={t("connector.privy.pwa.step2")}
            classNames={{
              content: "oui-bg-[#e9e9eb]",
            }}
            content={
              <Flex
                className="oui-w-full oui-bg-secondary oui-rounded-[12px]"
                direction="column"
              >
                <Flex
                  className="oui-w-full oui-px-4 oui-py-3"
                  justify={"start"}
                  itemAlign={"center"}
                >
                  <Text className="oui-text-[18px] oui-text-base-static-contrast-20">
                    {t("connector.privy.pwa.findOnPage")}
                  </Text>
                </Flex>
                <Divider className="oui-border-base-static-contrast-20 oui-w-full" />
                <Flex
                  className="oui-w-full oui-px-4 oui-py-3"
                  justify={"between"}
                  itemAlign={"center"}
                >
                  <Text className="oui-col-span-2 oui-whitespace-nowrap oui-text-[18px] oui-text-base-static-contrast-80">
                    {t("connector.privy.pwa.addToHomeScreen")}
                  </Text>
                  <AddIcon />
                </Flex>
              </Flex>
            }
          />
        </Flex>
      </Flex>
    </SimpleSheet>
  );
});

const ShareIcon = (props: { className?: string }) => {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="28"
      height="28"
      viewBox="0 0 28 28"
      fill="none"
      className={props.className}
    >
      <path
        d="M5 23.3333V13.6667C5 12.1939 6.15127 11 7.57143 11H10.6744V12.7778H7.57143C7.09804 12.7778 6.71429 13.1757 6.71429 13.6667V23.3333C6.71429 23.8243 7.09804 24.2222 7.57143 24.2222H20.4286C20.902 24.2222 21.2857 23.8243 21.2857 23.3333V13.6667C21.2857 13.1757 20.902 12.7778 20.4286 12.7778H17.555V11H20.4286C21.8487 11 23 12.1939 23 13.6667V23.3333C23 24.8061 21.8487 26 20.4286 26H7.57143C6.15127 26 5 24.8061 5 23.3333Z"
        fill="black"
        fillOpacity="0.48"
      />
      <path
        d="M13.3759 2.25933C13.7206 1.91356 14.2794 1.91356 14.6241 2.25933L20.2416 7.89432C20.586 8.24011 20.5862 8.80066 20.2416 9.14634C19.897 9.49203 19.3382 9.49185 18.9935 9.14634L14.8827 5.02278V19H13.1173V5.02278L9.00654 9.14634C8.66182 9.49185 8.10301 9.49203 7.7584 9.14634C7.41378 8.80066 7.41396 8.24011 7.7584 7.89432L13.3759 2.25933Z"
        fill="black"
        fillOpacity="0.48"
      />
    </svg>
  );
};

const AddIcon = () => {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="24"
      height="24"
      viewBox="0 0 24 24"
      fill="none"
    >
      <path
        d="M7.00598 3.92969H17.006C18.6627 3.92969 20.006 5.27297 20.006 6.92969V16.9297C20.006 18.5864 18.6627 19.9297 17.006 19.9297H7.00598C5.34927 19.9297 4.00598 18.5864 4.00598 16.9297V6.92969C4.00598 5.27297 5.34927 3.92969 7.00598 3.92969Z"
        stroke="black"
        strokeOpacity="0.88"
        strokeWidth="2"
      />
      <path
        d="M8 12L16 12"
        stroke="black"
        strokeOpacity="0.88"
        strokeWidth="2"
        strokeLinecap="round"
      />
      <path
        d="M12 8L12 16"
        stroke="black"
        strokeOpacity="0.88"
        strokeWidth="2"
        strokeLinecap="round"
      />
    </svg>
  );
};

const StepItem = (props: {
  index: number;
  title: string;
  content: React.ReactNode;
  classNames?: {
    content?: string;
  };
}) => {
  return (
    <Flex
      className=" oui-rounded-[12px] oui-w-full oui-overflow-hidden"
      direction="column"
    >
      <Flex className="oui-bg-base-4 oui-w-full oui-px-3 oui-py-3 oui-gap-[6px]">
        <Text className="oui-text-base-contrast-80 oui-text-xs oui-w-[18px] oui-h-[18px] oui-rounded-[100%] oui-bg-[linear-gradient(270deg,rgb(var(--oui-gradient-brand-end))_0%,rgb(var(--oui-gradient-brand-start))_100%)] oui-text-center oui-text-base-static-contrast">
          {props.index}
        </Text>
        <Text className="oui-text-base-contrast-80 oui-text-xs">
          {props.title}
        </Text>
      </Flex>
      <Flex
        className={cn(
          "oui-bg-secondary oui-h-[120px] oui-w-full oui-px-6",
          props.classNames?.content,
        )}
        justify={"center"}
        itemAlign={"center"}
      >
        {props.content}
      </Flex>
    </Flex>
  );
};
