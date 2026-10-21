import { Button } from "@layerx/ui";

export function PaymentScreen() {
  return (
    <main>
      <label>
        Amount
        <input inputMode="decimal" />
      </label>
      <Button>Continue</Button>
    </main>
  );
}
