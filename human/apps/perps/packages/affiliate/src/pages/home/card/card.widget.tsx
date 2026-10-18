import { useCardScript } from "./card.script";
import { Card } from "./card.ui";

export const CardWidget = () => {
  const state = useCardScript();
  return <Card {...state} />;
};
