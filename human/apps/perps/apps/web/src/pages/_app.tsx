import type { AppProps } from "next/app";
import "../styles/globals.css";
import "react-grid-layout/css/styles.css";
import "react-resizable/css/styles.css";
import "../../../../packages/layout-grid/src/styles.css";
export default function WebApp({Component,pageProps}:AppProps){return <Component {...pageProps}/>;}
