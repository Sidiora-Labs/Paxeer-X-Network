import { create } from "zustand";
import { persist } from "zustand/middleware";

export type SwapSupport = {
  data: Record<string, any> | null;
  loading: boolean;
  error: Error | null;
};

export type SwapSupportActions = {
  fetchData: () => Promise<Record<string, any> | null>;
};

export const useSwapSupportStore = create<SwapSupport & SwapSupportActions>()(
  persist(
    (set) => ({
      data: null,
      loading: false,
      error: null,

      fetchData: async () => {
        try {
          set({ loading: true, error: null });
          const res = await fetch("https://fi-api.woo.org/swap_support");
          const data = await res.json();
          if (data.status === "ok") {
            set({ data: data.data, loading: false, error: null });
            return data.data;
          }
          // When API returns error status, preserve existing data by not updating it
          set((state) => ({
            data: state.data ?? {},
            error: new Error(data.message),
            loading: false,
          }));
          return null;
        } catch (error) {
          console.log("!!!swap support error", error);
          // When fetch fails, preserve existing data by not updating it
          set((state) => ({
            data: state.data ?? {},
            error: error as Error,
            loading: false,
          }));
          return null;
        }
      },
    }),
    {
      name: "paxeer-network-swap-support", // Key for localStorage persistence
      partialize: (state) => ({ data: state.data }), // Only persist data field
    },
  ),
);
