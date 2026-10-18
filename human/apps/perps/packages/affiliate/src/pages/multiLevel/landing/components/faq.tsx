import React, { FC, Fragment, useCallback, useMemo, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  Flex,
  Text,
  Collapsible,
  CollapsibleContent,
  Box,
} from "@paxeer-network/ui";

type FaqItem = {
  id: string;
  number: number;
  question: string;
  answer: string;
  expanded?: boolean;
};

export const Faq = () => {
  const { t } = useTranslation();

  const faqItemsData = useMemo(() => {
    return [
      {
        id: "faq-1",
        question: t("affiliate.faq.question1"),
        answer: t("affiliate.faq.answer1"),
      },
      {
        id: "faq-2",
        question: t("affiliate.faq.question2"),
        answer: t("affiliate.faq.answer2"),
      },
      {
        id: "faq-3",
        question: t("affiliate.faq.question3"),
        answer: t("affiliate.faq.answer3"),
      },
      {
        id: "faq-4",
        question: t("affiliate.faq.question4"),
        answer: t("affiliate.faq.answer4"),
      },
      {
        id: "faq-5",
        question: t("affiliate.faq.question5"),
        answer: t("affiliate.faq.answer5"),
      },
    ] as FaqItem[];
  }, [t]);

  // State to track which items are expanded
  const [expandedItems, setExpandedItems] = useState<Set<string>>(new Set());

  // Toggle expanded state
  const toggleItem = useCallback((id: string) => {
    setExpandedItems((prev) => {
      const newSet = new Set(prev);
      if (newSet.has(id)) {
        newSet.delete(id);
      } else {
        newSet.add(id);
      }
      return newSet;
    });
  }, []);

  const faqItems = useMemo(() => {
    return faqItemsData.map((item) => ({
      ...item,
      expanded: expandedItems.has(item.id),
    }));
  }, [faqItemsData, expandedItems]);

  return (
    <Box className="oui-affiliate-faq">
      {/* Title */}
      <Text className="oui-text-[32px]">{t("affiliate.faq.title")}</Text>

      {/* FAQ Items */}
      <Flex
        direction="column"
        itemAlign="stretch"
        gap={0}
        className="oui-faq-list oui-mt-[clamp(1.5rem,2vw,2rem)]"
      >
        {faqItems.map((item, index) => (
          <Fragment key={item.id}>
            <Collapsible
              open={item.expanded}
              onOpenChange={() => toggleItem(item.id)}
            >
              <Flex
                direction="column"
                className="oui-cursor-pointer oui-py-4"
                itemAlign="stretch"
                onClick={() => toggleItem(item.id)}
              >
                {/* Question row */}
                <Flex justify="between" itemAlign="center" gap={4}>
                  <Flex gap={2} itemAlign="center" className="oui-flex-1">
                    <Text size="2xl" weight="semibold">
                      {`${index + 1}. ${item.question}`}
                    </Text>
                  </Flex>
                  <ChevronIcon expanded={item.expanded} />
                </Flex>

                {/* Answer (collapsible) */}
                <CollapsibleContent>
                  <Box className="oui-pt-3 oui-text-base-contrast-54 oui-font-medium">
                    {item.answer}
                  </Box>
                </CollapsibleContent>
              </Flex>
            </Collapsible>

            {/* Divider (except for last item) */}
            {index < faqItems.length - 1 && (
              <div className="oui-my-4 oui-h-px oui-bg-line-6" />
            )}
          </Fragment>
        ))}
      </Flex>
    </Box>
  );
};

// Chevron icon component
const ChevronIcon: FC<{ expanded: boolean }> = ({ expanded }) => (
  <svg
    width="24"
    height="24"
    viewBox="0 0 16 16"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    className={`oui-transition-transform oui-duration-200 ${
      expanded ? "oui-rotate-180" : ""
    }`}
  >
    <path
      d="M4 6L8 10L12 6"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      className="oui-text-base-contrast-54"
    />
  </svg>
);
