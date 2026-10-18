// import { act, renderHook } from "@testing-library/react";
// import { useTaskProfitAndStopLossInternal } from "../useTPSL";

// describe("useTakeProfitAndStopLoss hook test", () => {
//   test("should return the correct initial values", async () => {
//     const { result } = renderHook(
//       () => {
//         return useTaskProfitAndStopLossInternal(
//           {
//             symbol: "PERP_ETH_USDC",
//           },
//           73000
//         );
//       },
//       {
//         // wrapper,
//       }
//     );

//     expect(result.current[0].symbol).toBe("PERP_ETH_USDC");
//     expect(result.current[1].error).toBe(null);
//     expect(typeof result.current[1].submit).toBe("function");
//   });

//   test("should update the order correctly", async () => {
//     const { result } = renderHook(
//       () => {
//         return useTaskProfitAndStopLossInternal(
//           {
//             symbol: "PERP_ETH_USDC",
//             average_open_price: 66000,
//             position_qty: 0.1,
//           },
//           73000
//         );
//       },
//       {
//         // wrapper,
//       }
//     );

//     act(() => {
//       result.current[1].setValue("tp_trigger_price", 67000);
//     });

//     expect(result.current[0].tp_trigger_price).toBe(67000);
//     expect(result.current[0].tp_offset).toBe(1000);

//     expect(result.current[0].tp_offset_percentage).toBe(0.015151515151515152);
//     expect(result.current[0].tp_pnl).toBe(100);

//     expect(result.current[0].sl_pnl).toBeUndefined();

//     // update stop loss price;
//     act(() => {
//       result.current[1].setValue("sl_trigger_price", 65000);
//     });

//     expect(result.current[0].sl_trigger_price).toBe(65000);
//     expect(result.current[0].sl_offset).toBe(1000);
//     expect(result.current[0].sl_offset_percentage).toBe(0.015151515151515152);
//     expect(result.current[0].sl_pnl).toBe(-100);

//     // update take profit offset

//     act(() => {
//       result.current[1].setValue("tp_offset", 5000);
//     });

//     expect(result.current[0].tp_trigger_price).toBe(71000);
//     expect(result.current[0].tp_offset).toBe(5000);
//     expect(result.current[0].tp_offset_percentage).toBe(0.07575757575757576);
//     expect(result.current[0].tp_pnl).toBe(500);

//     //----- the stop loss should not be updated
//     expect(result.current[0].sl_trigger_price).toBe(65000);
//     expect(result.current[0].sl_offset).toBe(1000);
//     expect(result.current[0].sl_offset_percentage).toBe(0.015151515151515152);
//     expect(result.current[0].sl_pnl).toBe(-100);
//   });
// });
