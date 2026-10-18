import React, { useMemo } from "react";
import { cnBase } from "tailwind-variants";
import { ButtonProps, buttonVariants } from "../button";
import { Flex } from "../flex";
import { useDocumentDirection } from "../hooks";
import { ChevronLeftIcon, ChevronRightIcon } from "../icon";
import { useLocale } from "../locale";
import { Select } from "../select";
import { Text } from "../typography/text";

const Pagination = ({ className, ...props }: React.ComponentProps<"nav">) => (
  <nav
    role="navigation"
    aria-label="pagination"
    className={cnBase(
      "oui-flex oui-justify-between oui-items-center oui-w-full oui-text-xs",
      className,
    )}
    {...props}
  />
);
Pagination.displayName = "Pagination";

const PaginationContent = React.forwardRef<
  HTMLUListElement,
  React.ComponentProps<"ul">
>(({ className, ...props }, ref) => (
  <ul
    ref={ref}
    className={cnBase(
      "oui-flex oui-flex-row oui-items-center oui-gap-x-2",
      className,
    )}
    {...props}
  />
));
PaginationContent.displayName = "PaginationContent";

const PaginationItem = React.forwardRef<
  HTMLLIElement,
  React.ComponentProps<"li">
>(({ className, ...props }, ref) => (
  <li ref={ref} className={cnBase("oui-leading-[0px]", className)} {...props} />
));
PaginationItem.displayName = "PaginationItem";

type PaginationLinkProps = {
  isActive?: boolean;
} & Pick<ButtonProps, "size"> &
  React.ComponentProps<"button">;

const PaginationLink = ({
  className,
  isActive,
  // size = "icon",
  ...props
}: PaginationLinkProps) => (
  <button
    aria-current={isActive ? "page" : undefined}
    data-active={isActive}
    className={buttonVariants({
      size: "xs",
      // color:'white',
      variant: isActive ? "contained" : "text",
      className:
        "oui-min-w-6 oui-text-primary-contrast oui-font-semibold data-[active=false]:hover:oui-bg-base-6 disabled:oui-bg-transparent disabled:hover:oui-bg-transparent",
      // size,
    })}
    {...props}
  />
);
PaginationLink.displayName = "PaginationLink";

const PaginationPrevious = ({
  className,

  ...props
}: React.ComponentProps<typeof PaginationLink>) => (
  <PaginationLink
    aria-label="Go to previous page"
    className={cnBase("oui-gap-1 oui-ps-2.5", className)}
    {...props}
  >
    <PaginationPreviousIcon />
  </PaginationLink>
);
PaginationPrevious.displayName = "PaginationPrevious";

const PaginationNext = ({
  className,
  ...props
}: React.ComponentProps<typeof PaginationLink>) => (
  <PaginationLink
    aria-label="Go to next page"
    className={cnBase("oui-gap-1 oui-pe-2.5", className)}
    {...props}
  >
    <PaginationNextIcon />
  </PaginationLink>
);
PaginationNext.displayName = "PaginationNext";

const PaginationPreviousIcon = () => {
  const dir = useDocumentDirection();
  const Icon = dir === "rtl" ? ChevronRightIcon : ChevronLeftIcon;
  return <Icon className="oui-h-4 oui-w-4" color="white" />;
};

const PaginationNextIcon = () => {
  const dir = useDocumentDirection();
  const Icon = dir === "rtl" ? ChevronLeftIcon : ChevronRightIcon;
  return <Icon className="oui-h-4 oui-w-4" color="white" />;
};

const PaginationEllipsis = ({
  className,
  ...props
}: React.ComponentProps<"span">) => {
  const [locale] = useLocale("pagination");
  return (
    <span
      aria-hidden
      className={cnBase(
        "oui-flex oui-h-9 oui-w-9 oui-items-center oui-justify-center",
        className,
      )}
      {...props}
    >
      {/* <DotsHorizontalIcon className="h-4 w-4" /> */}
      <span className="sr-only">{locale.morePages}</span>
    </span>
  );
};
PaginationEllipsis.displayName = "PaginationEllipsis";

export type PaginationProps = {
  onPageChange?: (page: number) => void;
  onPageSizeChange?: (pageSize: number) => void;
  pageSize?: number;
  page: number;
  count: number;
  pageTotal: number;
  className?: string;
  classNames?: {
    pagination?: string;
    paginationContent?: string;
    paginationItem?: string;
    paginationLink?: string;
    paginationPrevious?: string;
    paginationNext?: string;
    paginationEllipsis?: string;
  };
  hideRowsPerPage?: boolean;
  generatePageNumbers?: (
    currentPage: number,
    totalPages: number,
  ) => (number | string)[];
};

const Paginations = (props: PaginationProps) => {
  const {
    classNames,
    className,
    pageTotal: totalPages,
    page: currentPage,
    hideRowsPerPage = false,
  } = props;

  const [locale] = useLocale("pagination");

  return (
    <Pagination className={cnBase(classNames?.pagination, className)}>
      {!hideRowsPerPage ? (
        <Flex className="oui-me-4">
          <Text
            as="div"
            size="2xs"
            intensity={54}
            className="oui-text-nowrap oui-me-2"
          >
            {locale.rowsPerPage}
          </Text>
          <div className={"oui-w-15"}>
            <Select.options
              options={[
                { value: "10", label: "10" },
                { value: "20", label: "20" },
                { value: "50", label: "50" },
                { value: "100", label: "100" },
              ]}
              value={`${props.pageSize ?? 5}`}
              size="xs"
              onValueChange={(value) =>
                props.onPageSizeChange?.(parseInt(value))
              }
            />
          </div>
        </Flex>
      ) : (
        <div></div>
      )}
      <PaginationItems {...props} />
    </Pagination>
  );
};

function generatePagination(
  currentPage: number,
  totalPages: number,
): (number | string)[] {
  const pagination: (number | string)[] = [];
  const ellipsis = "...";
  const maxPagesToShow = 5;

  if (totalPages <= maxPagesToShow) {
    for (let i = 1; i <= totalPages; i++) {
      pagination.push(i);
    }
  } else {
    if (currentPage <= 3) {
      pagination.push(1, 2, 3, 4, ellipsis, totalPages);
    } else if (currentPage >= totalPages - 2) {
      pagination.push(
        1,
        ellipsis,
        totalPages - 3,
        totalPages - 2,
        totalPages - 1,
        totalPages,
      );
    } else {
      pagination.push(
        1,
        ellipsis,
        currentPage - 1,
        currentPage,
        currentPage + 1,
        ellipsis,
        totalPages,
      );
    }
  }

  return pagination;
}

const PaginationItems = (props: Omit<PaginationProps, "onPageSizeChange">) => {
  const {
    classNames,
    className,
    pageTotal: totalPages,
    page: currentPage,
  } = props;

  const pageNumbers = useMemo(() => {
    if (typeof props.generatePageNumbers === "function") {
      return props.generatePageNumbers(currentPage, totalPages);
    }
    return generatePagination(currentPage, totalPages);
  }, [currentPage, totalPages, props.generatePageNumbers]);

  if (totalPages <= 1) {
    return null;
  }

  return (
    <PaginationContent>
      <PaginationItem>
        <PaginationPrevious
          // @ts-ignore
          disabled={props.page === 1}
          onClick={(event) => {
            event.preventDefault();
            props.onPageChange?.(props.page - 1);
          }}
        />
      </PaginationItem>
      {pageNumbers.map((page, index) => {
        return (
          <PaginationItem key={index}>
            <PaginationLink
              isActive={page === props.page}
              onClick={(event) => {
                event.preventDefault();
                if (page !== "...") {
                  props.onPageChange?.(Number(page));
                } else {
                  props.onPageChange?.(
                    Number((pageNumbers[index + 1] as number) - 1),
                  );
                }
              }}
            >
              {page}
            </PaginationLink>
          </PaginationItem>
        );
      })}

      {/* <PaginationItem>
          <PaginationEllipsis />
        </PaginationItem> */}
      <PaginationItem>
        <PaginationNext
          disabled={props.page === props.pageTotal}
          onClick={(event) => {
            event.preventDefault();
            props.onPageChange?.(props.page + 1);
          }}
        />
      </PaginationItem>
    </PaginationContent>
  );
};

export {
  Paginations,
  Pagination,
  PaginationContent,
  PaginationLink,
  PaginationItem,
  PaginationPrevious,
  PaginationNext,
  PaginationItems,
  PaginationEllipsis,
};
