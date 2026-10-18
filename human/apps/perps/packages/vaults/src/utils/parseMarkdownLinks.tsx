import { ReactNode } from "react";

/**
 * Parse markdown links [text](url) and convert to React elements
 * @param text - The text containing markdown links
 * @returns Array of React nodes with links converted to <a> elements
 */
export const parseMarkdownLinks = (text: string): ReactNode[] => {
  // Add defensive check: return empty string if text is undefined/null
  if (!text) {
    return [""];
  }

  const markdownLinkRegex = /\[([^\]]+)\]\(([^)]+)\)/g;
  const parts: ReactNode[] = [];
  let lastIndex = 0;
  let match;

  while ((match = markdownLinkRegex.exec(text)) !== null) {
    const [fullMatch, linkText, url] = match;
    const matchIndex = match.index;

    // Add text before the link
    if (matchIndex > lastIndex) {
      parts.push(text.substring(lastIndex, matchIndex));
    }

    // Add the link element with underline
    parts.push(
      <a
        key={matchIndex}
        href={url}
        target="_blank"
        rel="noopener noreferrer"
        className="oui-underline hover:oui-text-base-contrast-80"
      >
        {linkText}
      </a>,
    );

    lastIndex = matchIndex + fullMatch.length;
  }

  // Add remaining text after the last link
  if (lastIndex < text.length) {
    parts.push(text.substring(lastIndex));
  }

  return parts.length > 0 ? parts : [text];
};
