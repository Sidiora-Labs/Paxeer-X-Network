import React from "react";
import { useChannel, useEffect } from "storybook/preview-api";
import { EVENTS, VARIABLE_KEYS } from "./constants";
import { hexToRgb, isColorValue, rgbToHex } from "./utils";

export const withThemeBuilder = (StoryFn: any, context: any) => {
  const themeChangeHandler = (params: {
    changedValues: Record<string, string>;
  }) => {
    console.log("++++changedValues", params.changedValues);
    Object.entries(params.changedValues).forEach(([key, value]) => {
      const newValue = isColorValue(key) ? (hexToRgb(value) ?? "") : value;
      document.documentElement.style.setProperty(key, newValue as string);
    });
  };

  // TODO(hooks): Wrap this decorator body in a React component before using hooks.
  const emit = useChannel({
    [EVENTS.CHANGE]: themeChangeHandler,
  });

  // TODO(hooks): Wrap this decorator body in a React component before using hooks.
  useEffect(() => {
    const root = document.documentElement;

    const styles = getComputedStyle(root);

    const cssVariables: Record<string, string> = {};
    for (let i = 0; i < VARIABLE_KEYS.length; i++) {
      const property = VARIABLE_KEYS[i];

      let value = styles.getPropertyValue(property).trim();

      if (isColorValue(property)) {
        value = rgbToHex(value.split(" "));
      }

      cssVariables[property] = value;
    }

    console.log("++++cssVariables", cssVariables);

    emit(EVENTS.THEME_RESTORE, cssVariables);
  }, []);

  return StoryFn();
};
