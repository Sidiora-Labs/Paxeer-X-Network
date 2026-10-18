import { DisplayControlSettingInterface } from "../../type";

export interface IProps {
  displayControlState: DisplayControlSettingInterface;
  changeDisplayControlState: (state: DisplayControlSettingInterface) => void;
}
export type DisplayControl = {
  label: string;
  id: keyof DisplayControlSettingInterface;
};

/**
 * One desktop dropdown menu row in display-control panel.
 * `onCheckedChange` allows plugins to inject fully custom toggle behaviors.
 */
export type DesktopDisplayControlMenuItem = {
  id: string;
  label: string;
  checked?: boolean;
  disabled?: boolean;
  onCheckedChange?: (checked: boolean) => void;
};

/**
 * Interceptor props for desktop display-control menu list target.
 */
export type DesktopDisplayControlMenuListProps = {
  items: DesktopDisplayControlMenuItem[];
};
