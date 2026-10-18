import { renderHook } from "@testing-library/react-hooks";
import { usePrivateQuery } from "../src";

test("test usePrivateQuery hook", async () => {
  const { result, waitForValueToChange } = renderHook(() => {
    return usePrivateQuery<any>("/usercenter/account");
  });

  await waitForValueToChange(() => result.current.data, {
    timeout: 5000,
  });

  expect(result.current.data).toBeUndefined();
});
