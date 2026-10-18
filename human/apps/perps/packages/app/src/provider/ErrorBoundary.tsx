import { PropsWithChildren, Component, ErrorInfo, ReactNode } from "react";
import { Translation } from "@paxeer-network/i18n";
import { Button, cn, Flex, Text } from "@paxeer-network/ui";

type ErrorBoundaryState = {
  hasError: boolean;
  error?: Error;
  errorInfo?: ErrorInfo;
};

type ErrorBoundaryProps = PropsWithChildren<{
  className?: string;
  onError?: (error: Error, errorInfo: ErrorInfo) => void;
  icon?: ReactNode;
  title?: string;
  description?: string;
  refreshButtonText?: string;
  onRefresh?: () => void;
}>;

export class ErrorBoundary extends Component<
  ErrorBoundaryProps,
  ErrorBoundaryState
> {
  constructor(props: ErrorBoundaryProps) {
    super(props);
    this.state = { hasError: false };
  }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    this.setState({
      error,
      errorInfo,
    });

    // Log error to console in development
    if (process.env.NODE_ENV === "development") {
      console.error("ErrorBoundary caught an error:", error, errorInfo);
    }

    this.props.onError?.(error, errorInfo);

    // You can also log the error to an error reporting service here
    // Example: errorReportingService.logError(error, errorInfo);
  }

  render() {
    if (this.state.hasError) {
      return (
        <Translation>
          {(t) => (
            <Flex
              direction="column"
              width="100%"
              itemAlign="center"
              justify="center"
              className={cn("oui-h-screen oui-px-[10%]", this.props.className)}
            >
              {this.props.icon || <Icon />}
              <Text
                className={cn(
                  "oui-mt-10",
                  "oui-text-[32px] oui-leading-[40px]",
                  "md:oui-text-[48px] md:oui-leading-[56px]",
                )}
                intensity={80}
                weight="bold"
              >
                {this.props.title || t("errorBoundary.title")}
              </Text>
              <Text
                className={cn(
                  "oui-mt-2",
                  "oui-text-[16px] oui-leading-[24px]",
                  "md:oui-text-[20px] md:oui-leading-[28px]",
                  "oui-text-center",
                )}
                weight="semibold"
                intensity={36}
              >
                {this.props.description || t("errorBoundary.description")}
              </Text>
              <Button
                onClick={() => {
                  if (typeof this.props.onRefresh === "function") {
                    this.props.onRefresh();
                  } else {
                    window.location.reload();
                  }
                }}
                className="oui-mt-4 oui-font-semibold"
                variant="contained"
                size="md"
              >
                <RefreshIcon className="oui-me-1" />
                {this.props.refreshButtonText || t("errorBoundary.refresh")}
              </Button>
              {/* {process.env.NODE_ENV === "development" && this.state.error && (
            <details className="mt-4 p-4 bg-red-50 border border-red-200 rounded text-start max-w-2xl">
              <summary className="cursor-pointer font-medium text-red-800">
                Error Details
              </summary>
              <pre className="mt-2 text-sm text-red-700 overflow-auto">
                {this.state.error.toString()}
                {this.state.errorInfo?.componentStack}
              </pre>
            </details>
          )} */}
            </Flex>
          )}
        </Translation>
      );
    }

    return this.props.children;
  }
}

const Icon = (props: { className?: string }) => {
  return (
    <svg
      width="156"
      height="165"
      viewBox="0 0 156 165"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className={props.className}
    >
      <path
        d="M77.958 11.2581C117.408 11.2581 149.224 43.7623 149.225 83.635C149.225 123.508 117.408 156.013 77.958 156.013C38.5076 156.013 6.69141 123.508 6.69141 83.635C6.69161 43.7623 38.5077 11.2581 77.958 11.2581Z"
        stroke="url(#paint0_linear_32953_90176)"
        strokeWidth="12"
      />
      <path
        d="M74.1236 18.8531C74.8967 18.2272 76.0309 18.3465 76.6568 19.1196L99.9666 47.9101C100.593 48.6832 100.473 49.8174 99.7001 50.4433C98.927 51.0692 97.7928 50.9499 97.1669 50.1768L73.8571 21.3863C73.2312 20.6132 73.3505 19.479 74.1236 18.8531Z"
        fill="#394155"
      />
      <path
        d="M67.4775 153.345C66.5687 153.75 65.5041 153.341 65.0996 152.432L47.5699 113.05C47.1654 112.142 47.5742 111.077 48.483 110.672C49.3918 110.268 50.4564 110.677 50.8609 111.585L68.3906 150.967C68.7951 151.876 68.3863 152.941 67.4775 153.345Z"
        fill="#394155"
      />
      <path
        d="M87.1937 2.50608C88.9032 1.12201 91.4111 1.38582 92.7951 3.09532L117.65 33.7939C119.034 35.5033 118.77 38.0112 117.06 39.3952L106.349 48.0675C104.64 49.4515 102.132 49.1877 100.748 47.4782L75.8932 16.7797C74.5092 15.0702 74.773 12.5624 76.4825 11.1783L87.1937 2.50608Z"
        fill="url(#paint1_linear_32953_90176)"
      />
      <path
        d="M54.8689 163.161C52.8595 164.055 50.5054 163.152 49.6109 161.142L30.369 117.913C29.4745 115.904 30.3784 113.55 32.3879 112.655L38.8256 109.79C40.8351 108.895 43.1892 109.799 44.0836 111.809L63.3256 155.037C64.22 157.047 63.3161 159.401 61.3067 160.295L54.8689 163.161Z"
        fill="url(#paint2_linear_32953_90176)"
      />
      <path
        d="M21.0497 137.635C19.2748 138.425 17.1955 137.627 16.4054 135.852C15.6154 134.077 16.4138 131.998 18.1887 131.208L29.4767 126.183C31.2516 125.393 33.331 126.191 34.121 127.966C34.911 129.741 34.1127 131.821 32.3377 132.611L21.0497 137.635Z"
        fill="url(#paint3_linear_32953_90176)"
      />
      <path
        d="M31.0067 160.657C29.2318 161.447 27.1525 160.649 26.3625 158.874C25.5724 157.099 26.3708 155.02 28.1457 154.23L39.4338 149.205C41.2087 148.415 43.288 149.213 44.078 150.988C44.8681 152.763 44.0697 154.843 42.2948 155.633L31.0067 160.657Z"
        fill="url(#paint4_linear_32953_90176)"
      />
      <path
        d="M94.2979 17.9219C94.2979 19.7549 92.812 21.2408 90.979 21.2408C89.1461 21.2408 87.6602 19.7549 87.6602 17.9219C87.6602 16.0889 89.1461 14.603 90.979 14.603C92.812 14.603 94.2979 16.0889 94.2979 17.9219Z"
        fill="#07080A"
      />
      <path
        d="M106.243 32.5249C106.243 34.3579 104.757 35.8438 102.924 35.8438C101.091 35.8438 99.6055 34.3579 99.6055 32.5249C99.6055 30.692 101.091 29.2061 102.924 29.2061C104.757 29.2061 106.243 30.692 106.243 32.5249Z"
        fill="#07080A"
      />
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        d="M143.238 87.3294C143.283 86.3294 143.306 85.3235 143.306 84.3123C143.306 80.0077 142.89 75.8003 142.096 71.728C137.49 69.544 132.251 67.6307 127.118 66.8126C122.674 66.1042 117.636 66.1072 113.053 68.1318C108.166 70.2911 104.601 74.3958 102.851 80.1999C101.677 84.091 100.713 87.7506 99.857 90.9972L99.8569 90.9974C99.5483 92.1682 99.2539 93.2853 98.969 94.3402C97.8574 98.455 96.882 101.656 95.6795 104.319C94.5159 106.895 93.2041 108.812 91.5007 110.366C89.7881 111.929 87.3882 113.389 83.7037 114.64L87.9295 127.083C92.9507 125.378 97.0389 123.101 100.357 120.074C103.684 117.039 105.944 113.516 107.655 109.727C109.327 106.025 110.528 101.937 111.655 97.7671C111.977 96.5735 112.294 95.3715 112.616 94.1496L112.616 94.1488L112.616 94.1487C113.456 90.9644 114.33 87.646 115.432 83.9941C116.178 81.5196 117.297 80.6226 118.363 80.1517C119.734 79.5462 121.92 79.2905 125.05 79.7894C131.168 80.7644 138.314 84.1982 143.238 87.3294ZM13.9805 70.6827C14.9192 66.2565 16.3058 61.9957 18.0906 57.9501C23.709 58.5554 30.4365 58.6747 37.3538 57.7247C48.6584 56.1721 59.8141 51.8684 67.7619 42.9131L77.5901 51.6356C66.8783 63.7052 52.3551 68.9284 39.1417 70.7431C29.9766 72.0018 21.1264 71.6711 13.9805 70.6827Z"
        fill="#394155"
      />
      <path
        d="M68.7641 23.3532C70.4736 21.9692 72.9814 22.233 74.3655 23.9425L94.9298 49.3421C96.3139 51.0515 96.0501 53.5594 94.3406 54.9434L86.1984 61.5356C84.4889 62.9197 81.9811 62.6559 80.597 60.9464L60.0326 35.5468C58.6486 33.8373 58.9124 31.3295 60.6219 29.9454L68.7641 23.3532Z"
        fill="#535E7B"
      />
      <path
        d="M74.0447 150.372C72.0352 151.267 69.6811 150.363 68.7867 148.353L53.0318 112.959C52.1373 110.949 53.0412 108.595 55.0507 107.7L72.5225 99.9234C74.532 99.029 76.8861 99.9329 77.7806 101.942L93.5355 137.337C94.4299 139.347 93.526 141.701 91.5165 142.595L74.0447 150.372Z"
        fill="#535E7B"
      />
      <defs>
        <linearGradient
          id="paint0_linear_32953_90176"
          x1="71.7422"
          y1="138.236"
          x2="124.758"
          y2="41.3888"
          gradientUnits="userSpaceOnUse"
        >
          <stop stopColor="#1B1D22" />
          <stop offset="1" stopColor="#26292E" />
        </linearGradient>
        <linearGradient
          id="paint1_linear_32953_90176"
          x1="111.705"
          y1="43.7314"
          x2="81.8381"
          y2="6.84218"
          gradientUnits="userSpaceOnUse"
        >
          <stop stopColor="rgb(var(--oui-gradient-brand-end))" />
          <stop offset="1" stopColor="rgb(var(--oui-gradient-brand-start))" />
        </linearGradient>
        <linearGradient
          id="paint2_linear_32953_90176"
          x1="35.6067"
          y1="111.223"
          x2="58.0878"
          y2="161.728"
          gradientUnits="userSpaceOnUse"
        >
          <stop stopColor="rgb(var(--oui-gradient-brand-end))" />
          <stop offset="1" stopColor="rgb(var(--oui-gradient-brand-start))" />
        </linearGradient>
        <linearGradient
          id="paint3_linear_32953_90176"
          x1="23.8327"
          y1="128.695"
          x2="26.6937"
          y2="135.123"
          gradientUnits="userSpaceOnUse"
        >
          <stop stopColor="rgb(var(--oui-gradient-brand-end))" />
          <stop offset="1" stopColor="rgb(var(--oui-gradient-brand-start))" />
        </linearGradient>
        <linearGradient
          id="paint4_linear_32953_90176"
          x1="33.7897"
          y1="151.717"
          x2="36.6508"
          y2="158.145"
          gradientUnits="userSpaceOnUse"
        >
          <stop stopColor="rgb(var(--oui-gradient-brand-end))" />
          <stop offset="1" stopColor="rgb(var(--oui-gradient-brand-start))" />
        </linearGradient>
      </defs>
    </svg>
  );
};

const RefreshIcon = (props: { className?: string }) => {
  return (
    <svg
      width="15"
      height="15"
      viewBox="0 0 15 15"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className={props.className}
    >
      <path
        d="M7.5026 2.52515C6.30444 2.52515 5.15994 2.92706 4.23944 3.65565C3.98686 3.85515 3.94835 4.22322 4.14844 4.47581C4.34852 4.72839 4.71602 4.76689 4.96861 4.56681C5.68436 4.00039 6.56985 3.69181 7.5026 3.69181C9.75777 3.69181 11.5859 5.51998 11.5859 7.77514H10.4193L12.1693 10.1085L13.9193 7.77514H12.7526C12.7526 4.87539 10.4024 2.52515 7.5026 2.52515ZM2.83594 5.44181L1.08594 7.77514H2.2526C2.2526 10.6749 4.60285 13.0251 7.5026 13.0251C8.70135 13.0251 9.84527 12.6238 10.7658 11.8946C11.0184 11.6951 11.0569 11.3271 10.8568 11.0745C10.6567 10.8219 10.2892 10.7834 10.0366 10.9835C9.32027 11.5505 8.43594 11.8585 7.5026 11.8585C5.24744 11.8585 3.41927 10.0303 3.41927 7.77514H4.58594L2.83594 5.44181Z"
        fill="black"
      />
    </svg>
  );
};
