import { useRefereeDescriptionFormScript } from "./refereeDescriptionForm.script";
import { RefereeDescriptionForm } from "./refereeDescriptionForm.ui";

export type RefereeDescriptionFormWidgetProps = {
  close?: () => void;
  onSuccess?: () => void | Promise<void>;
  address: string;
  description?: string | null;
  initialDescription?: string | null;
};

export const RefereeDescriptionFormWidget = (
  props: RefereeDescriptionFormWidgetProps,
) => {
  const state = useRefereeDescriptionFormScript(props);
  return (
    <RefereeDescriptionForm
      {...props}
      {...state}
      initialDescription={props.description}
    />
  );
};
