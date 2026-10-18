import { useEffect, useState } from "react";
import { useAccount, useConnect } from "@paxeer-network/paxeer-backend/walletRuntime";
import {
  usePaxeer,
  PAXEER_CONNECTOR_ID,
} from "@paxeer-network/paxeer-backend/core/wallet/embedded";
import { SimpleDialog, Button, Flex, Text, Input, toast } from "@paxeer-network/ui";
import { ChainNamespace } from "@paxeer-network/types";
export function NativeWalletDialog() {
  const [request, setRequest] = useState<any>(null);
  const [email, setEmail] = useState("");
  const [emailSent, setEmailSent] = useState(false);
  const { connectors, connectAsync, isPending } = useConnect();
  const wallet = useAccount();
  const auth = usePaxeer();
  useEffect(() => {
    const listener = (event: Event) => {
      setEmailSent(false);
      setRequest((event as CustomEvent).detail);
    };
    window.addEventListener("paxeer-network:wallet-picker", listener);
    return () => window.removeEventListener("paxeer-network:wallet-picker", listener);
  }, []);
  const connect = async (connector: (typeof connectors)[number]) => {
    try {
      const result = await connectAsync({ connector });
      const provider = await connector.getProvider();
      request?.resolve([
        {
          label: connector.name,
          icon: "/pax-white.svg",
          provider,
          accounts: result.accounts.map((address) => ({ address })),
          chains: [{ id: result.chainId, namespace: ChainNamespace.evm }],
        },
      ]);
      setRequest(null);
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : "Unable to connect wallet.",
      );
    }
  };
  useEffect(() => {
    if (!auth.isAuthenticated || !auth.publicWallet || wallet.isConnected)
      return;
    const connector = connectors.find(
      (item) => item.id === PAXEER_CONNECTOR_ID,
    );
    if (connector) void connect(connector);
  }, [auth.isAuthenticated, auth.publicWallet, wallet.isConnected]);
  const close = () => {
    request?.reject(new Error("Wallet connection cancelled."));
    setRequest(null);
  };
  return (
    <SimpleDialog
      open={Boolean(request)}
      onOpenChange={(open) => {
        if (!open) close();
      }}
      title="Connect wallet"
      size="sm"
    >
      <Flex direction="column" gap={3} itemAlign="stretch">
        {auth.wallet && !auth.isAuthenticated && (
          <>
            <Input
              type="email"
              value={email}
              onValueChange={setEmail}
              placeholder="Email address"
            />
            <Button
              loading={auth.authBusy}
              disabled={!email.includes("@")}
              onClick={async () => {
                const result = await auth.signInWithEmail(email);
                if (result.ok) setEmailSent(true);
                else toast.error(result.error ?? "Unable to sign in.");
              }}
            >
              Continue with email
            </Button>
            {emailSent && (
              <Text size="xs">Check your email for the sign-in link.</Text>
            )}
            <Flex gap={2}>
              <Button
                fullWidth
                variant="outlined"
                loading={auth.authBusy}
                onClick={() =>
                  void auth
                    .signInWithOAuth("google")
                    .catch((error) => toast.error(error.message))
                }
              >
                Google
              </Button>
              <Button
                fullWidth
                variant="outlined"
                loading={auth.authBusy}
                onClick={() =>
                  void auth
                    .signInWithOAuth("github")
                    .catch((error) => toast.error(error.message))
                }
              >
                GitHub
              </Button>
            </Flex>
          </>
        )}
        {connectors
          .filter((connector) => connector.id !== PAXEER_CONNECTOR_ID)
          .map((connector) => (
            <Button
              key={connector.uid}
              fullWidth
              loading={isPending}
              variant="outlined"
              onClick={() => void connect(connector)}
            >
              {connector.name}
            </Button>
          ))}
        {auth.isAuthenticated && (
          <Button
            loading={isPending}
            onClick={() => {
              const connector = connectors.find(
                (item) => item.id === PAXEER_CONNECTOR_ID,
              );
              if (connector) void connect(connector);
            }}
          >
            Paxeer wallet
          </Button>
        )}
        <Text size="xs" intensity={54}>
          Connect your wallet to use paxeer-network on Paxeer.
        </Text>
      </Flex>
    </SimpleDialog>
  );
}
