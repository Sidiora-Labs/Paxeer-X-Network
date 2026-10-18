import React from "react";
import { addons, types } from "storybook/manager-api";
import { ADDON_ID, PANEL_ID } from "./constants";
import { Panel } from "./panel";

addons.register(ADDON_ID, () => {
  addons.add(PANEL_ID, {
    type: types.PANEL,
    title: "Theme Builder",
    match: ({ viewMode }) => viewMode === "story",
    render: Panel,
  });
});
