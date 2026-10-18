import { FC } from "react";
import { cn } from "@paxeer-network/ui";

// import ReactMarkdown from "react-markdown";
// import remarkGfm from "remark-gfm";

// Enhanced type definition supporting multiple content formats
type ListStyle =
  | "disc"
  | "decimal"
  | "none"
  | "circle"
  | "square"
  | "decimal-leading-zero"
  | "lower-alpha"
  | "upper-alpha"
  | "lower-roman"
  | "upper-roman";

export type DescriptionItem = {
  content: string; // Support markdown syntax, plain text, or image URL
  type: "markdown" | "text" | "image";
  alt?: string; // For images
  className?: string; // Custom styling
  listStyle?: ListStyle; // List style for ul/ol elements
  listClassName?: string; // Custom list container class
  children?: DescriptionItem[];
};

export type DescriptionConfig = {
  listStyle?: ListStyle; // Global list style
  listClassName?: string; // Global list container class
};

// Utility function to get list style classes
const getListStyleClass = (style: ListStyle): string => {
  const styleMap: Record<ListStyle, string> = {
    disc: "oui-list-disc",
    decimal: "oui-list-decimal",
    none: "oui-list-none",
    circle: "oui-list-disc", // Tailwind doesn't have circle, fallback to disc
    square: "oui-list-disc", // Tailwind doesn't have square, fallback to disc
    "decimal-leading-zero": "oui-list-decimal",
    "lower-alpha": "oui-list-decimal", // Fallback
    "upper-alpha": "oui-list-decimal", // Fallback
    "lower-roman": "oui-list-decimal", // Fallback
    "upper-roman": "oui-list-decimal", // Fallback
  };
  return styleMap[style] || "oui-list-disc";
};

// Custom rich text parser (no external dependencies)
const parseRichText = (text: string): JSX.Element[] => {
  const parts: JSX.Element[] = [];
  let currentIndex = 0;
  let key = 0;

  // Parse **bold** syntax
  const boldRegex = /\*\*(.*?)\*\*/g;
  // Parse [link text](url) syntax
  const linkRegex = /\[([^\]]+)\]\(([^)]+)\)/g;
  // Parse ![alt text](image url) syntax
  const imageRegex = /!\[([^\]]*)\]\(([^)]+)\)/g;

  const allMatches: Array<{
    type: "bold" | "link" | "image";
    match: RegExpMatchArray;
    index: number;
  }> = [];

  // Find all matches
  let match;
  while ((match = boldRegex.exec(text)) !== null) {
    allMatches.push({ type: "bold", match, index: match.index });
  }
  while ((match = linkRegex.exec(text)) !== null) {
    allMatches.push({ type: "link", match, index: match.index });
  }
  while ((match = imageRegex.exec(text)) !== null) {
    allMatches.push({ type: "image", match, index: match.index });
  }

  // Sort by index
  allMatches.sort((a, b) => a.index - b.index);

  for (const { type, match } of allMatches) {
    // Add text before match
    if (match.index !== undefined && match.index > currentIndex) {
      const beforeText = text.slice(currentIndex, match.index);
      if (beforeText) {
        parts.push(<span key={key++}>{beforeText}</span>);
      }
    }

    // Add formatted element
    switch (type) {
      case "bold":
        parts.push(
          <strong
            key={key++}
            className="oui-font-bold oui-text-base-contrast-54"
          >
            {match[1]}
          </strong>,
        );
        break;
      case "link":
        parts.push(
          <a
            key={key++}
            href={match[2]}
            className="oui-text-base-contrast-54 oui-underline hover:oui-text-base-contrast-54"
            target="_blank"
            rel="noopener noreferrer"
          >
            {match[1]}
          </a>,
        );
        break;
      case "image":
        parts.push(
          <img
            key={key++}
            src={match[2]}
            alt={match[1]}
            className="oui-my-2 oui-block oui-h-auto oui-max-w-full"
          />,
        );
        break;
    }

    currentIndex = (match.index ?? 0) + match[0].length;
  }

  // Add remaining text
  if (currentIndex < text.length) {
    const remainingText = text.slice(currentIndex);
    if (remainingText) {
      parts.push(<span key={key++}>{remainingText}</span>);
    }
  }

  return parts.length > 0 ? parts : [<span key={0}>{text}</span>];
};

// Custom rich text implementation (recommended for lightweight needs)
export const DescriptionContent: FC<{
  description: DescriptionItem[] | ReadonlyArray<DescriptionItem>;
  config?: DescriptionConfig;
}> = ({ description, config }) => {
  const renderContent = (
    contents: DescriptionItem[] | ReadonlyArray<DescriptionItem>,
    level: number = 0,
  ) => {
    // Determine list style - item level overrides global config
    const defaultListStyle = config?.listStyle || "disc";
    const hasListItems = contents.some((item) => item.children?.length);

    if (!hasListItems && contents.length === 1) {
      // Single item without children - render without list wrapper
      const content = contents[0];
      return (
        <div className={cn("oui-mb-2", content.className)}>
          {renderContentItem(content)}
        </div>
      );
    }

    return (
      <ul
        className={cn(
          // Use list-outside for proper alignment
          "oui-list-outside",
          // Add left padding for proper spacing
          level === 0 ? "oui-ps-5" : "oui-ps-4",
          // Get list style class
          getListStyleClass(defaultListStyle),
          // Custom list container class
          config?.listClassName,
        )}
      >
        {contents.map((content, index) => {
          const itemListStyle = content.listStyle || defaultListStyle;

          return (
            <li
              key={`${content.content}-${index}`}
              className={cn(
                "oui-mb-1 oui-leading-relaxed",
                // Override list style if specified on item
                content.listStyle && getListStyleClass(itemListStyle),
                content.className,
              )}
            >
              {renderContentItem(content)}
              {content?.children?.length && (
                <div className="oui-mt-2 oui-text-sm oui-text-base-contrast-36">
                  {renderContent(content.children, level + 1)}
                </div>
              )}
            </li>
          );
        })}
      </ul>
    );
  };

  const renderContentItem = (content: DescriptionItem) => {
    if (content.type === "image") {
      return (
        <img
          src={content.content}
          alt={content.alt || ""}
          className="oui-my-2 oui-h-auto oui-max-w-full"
        />
      );
    }

    // Both markdown and text use the rich text parser
    return (
      <div className="rich-text-content inline">
        {parseRichText(content.content)}
      </div>
    );
  };

  return (
    <div
      className={cn(
        "oui-text-sm oui-font-semibold oui-leading-[28px] oui-text-base-contrast-54",
      )}
    >
      {renderContent(description)}
    </div>
  );
};

// Alternative: React-markdown implementation (requires additional dependencies)
export const MarkdownDescriptionContent: FC<{
  description: DescriptionItem[] | ReadonlyArray<DescriptionItem>;
}> = ({ description }) => {
  const renderContent = (
    contents: DescriptionItem[] | ReadonlyArray<DescriptionItem>,
  ) => {
    return (
      <ul className="oui-list-inside oui-list-disc">
        {contents.map((content, index) => (
          <li key={`${content.content}-${index}`} className={content.className}>
            {content.type === "image" ? (
              <img
                src={content.content}
                alt={content.alt || ""}
                className="oui-my-2 oui-h-auto oui-max-w-full"
              />
            ) : (
              <div>{content.content}</div>
            )}
            {Array.isArray(content?.children) &&
              content?.children.length > 0 && (
                <div className="oui-ms-4">
                  {renderContent(content.children)}
                </div>
              )}
          </li>
        ))}
      </ul>
    );
  };

  return (
    <div
      className={cn(
        "oui-text-sm oui-font-semibold oui-leading-[28px] oui-text-base-contrast-54",
      )}
    >
      {renderContent(description)}
    </div>
  );
};
