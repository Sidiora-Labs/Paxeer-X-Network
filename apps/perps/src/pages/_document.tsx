import { Html, Head, Main, NextScript } from 'next/document';

export default function Document() {
  return (
    <Html>
      <Head>
        <meta
          name="build-id"
          content={process.env.NEXT_PUBLIC_VERCEL_GIT_COMMIT_SHA ?? 'development'}
        />
      </Head>
      <body>
        <Main />
        <NextScript />
      </body>
    </Html>
  );
}