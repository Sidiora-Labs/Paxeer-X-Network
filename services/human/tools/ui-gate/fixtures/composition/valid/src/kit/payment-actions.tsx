import { Button, Input } from "@layerx/ui";

export function PaymentActions() {
  return (
    <section>
      <label>
        Amount
        <Input inputMode="decimal" />
      </label>
      <Button>Continue</Button>
    </section>
  );
}
