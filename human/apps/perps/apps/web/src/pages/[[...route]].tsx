import dynamic from "next/dynamic";
import Head from "next/head";
const PaxeerNetwork = dynamic(() => import("../native/PaxeerNetwork"), {
  ssr: false,
});
export default function Page() {
  return (
    <>
      <Head>
        <title>Paxeer Xtrades</title>
        <meta name="application-name" content="Paxeer Xtrades" />
        <link rel="icon" type="image/png" href="/brand/paxeer-x.png" />
        <link rel="apple-touch-icon" href="/brand/paxeer-x.png" />
        <meta name="theme-color" content="#06090f" />
      </Head>
      <PaxeerNetwork />
    </>
  );
}
