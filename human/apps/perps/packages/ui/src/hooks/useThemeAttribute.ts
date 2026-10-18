import { useEffect, useState } from "react";

/**
 * get the data-oui-theme attribute from the <html> element
 * @returns the data-oui-theme attribute
 */
export const useThemeAttribute = () => {
  const [themeAttribute, setThemeAttribute] = useState("");

  // watch the data-oui-theme attribute on the <html> element
  useEffect(() => {
    setThemeAttribute(
      document.documentElement.getAttribute("data-oui-theme") ?? "",
    );

    const observer = new MutationObserver((mutations) => {
      for (const mutation of mutations) {
        if (
          mutation.type === "attributes" &&
          mutation.attributeName === "data-oui-theme"
        ) {
          setThemeAttribute(
            document.documentElement.getAttribute("data-oui-theme") ?? "",
          );
          break;
        }
      }
    });

    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-oui-theme"],
    });
    return () => observer?.disconnect();
  }, []);

  return themeAttribute;
};
