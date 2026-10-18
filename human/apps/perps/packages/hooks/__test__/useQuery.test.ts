import { renderHook } from "@testing-library/react-hooks";
import { useQuery } from "../src";

test("useQuery", async () => {
  const { result, waitForValueToChange } = renderHook(() => {
    return useQuery("/public/funding_rate");
  });

  await waitForValueToChange(() => result.current.data, {
    timeout: 5000,
  });

  expect(result.current.data).toBeUndefined();
});
