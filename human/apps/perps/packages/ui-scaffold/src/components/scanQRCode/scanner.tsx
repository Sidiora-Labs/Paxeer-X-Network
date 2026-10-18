import { FC, useEffect, useRef } from "react";
import jsQR from "jsqr";

type ScannerProps = {
  onSuccess?: (data: string) => void;
};

export const QRCODE_WIDTH = 320;
export const QRCODE_HEIGHT = 320;
const RATIO = 2;

/**
 * QR Code Scanner
 * reference https://github.com/cozmo/jsQR/blob/master/docs/index.html
 */
export const QRCodeScanner: FC<ScannerProps> = (props) => {
  const videoRef = useRef<HTMLVideoElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);

  const tick = () => {
    const video = videoRef.current;
    const canvas = canvasRef.current;
    if (!video || !canvas) return;

    const ctx = canvas.getContext("2d")!;

    if (video.readyState === video.HAVE_ENOUGH_DATA) {
      const videoWidth = video.videoWidth;
      const videoHeight = video.videoHeight;

      const canvasAspectRatio = canvas.width / canvas.height;
      const videoAspectRatio = videoWidth / videoHeight;

      let sx, sy, sWidth, sHeight;

      if (videoAspectRatio > canvasAspectRatio) {
        // The video is wide. Crop the left and right sides
        sHeight = videoHeight;
        sWidth = videoHeight * canvasAspectRatio;
        sx = (videoWidth - sWidth) / 2;
        sy = 0;
      } else {
        // The video is high. Crop the top and bottom sides
        sWidth = videoWidth;
        sHeight = videoWidth / canvasAspectRatio;
        sx = 0;
        sy = (videoHeight - sHeight) / 2;
      }

      ctx.drawImage(
        video,
        sx,
        sy,
        sWidth,
        sHeight,
        0,
        0,
        canvas.width,
        canvas.height,
      );

      // ctx.drawImage(video, 0, 0, canvas.width, canvas.height);
      const imageData = ctx.getImageData(0, 0, canvas.width, canvas.height);
      const res = jsQR(imageData.data, imageData.width, imageData.height, {
        inversionAttempts: "dontInvert",
      });

      if (res?.data) {
        console.log("scan result", res.data);
        props.onSuccess?.(res.data);
      }
    }
    requestAnimationFrame(tick);
  };

  useEffect(() => {
    const video = videoRef.current;
    if (!open || !video) return;

    // Use facingMode: environment to attemt to get the front camera on phones
    navigator?.mediaDevices
      ?.getUserMedia({
        video: {
          facingMode: "environment",
          width: { ideal: QRCODE_WIDTH * RATIO },
          height: { ideal: QRCODE_HEIGHT * RATIO },
        },
      })
      .then((stream) => {
        video.srcObject = stream;
        // required to tell iOS safari we don't want fullscreen
        video.setAttribute("playsinline", "true");
        video.play();

        video.onloadeddata = () => {
          console.log("Video data loaded.");
          // const videoTrack = stream.getVideoTracks()[0];
          // const settings = videoTrack.getSettings();
          // console.log("video size", `${settings.width}x${settings.height}`);

          requestAnimationFrame(tick);
        };
      })
      .catch((err) => {
        console.error("Error accessing camera:", err);
      });

    return () => {
      // Cleanup: stop video stream when component unmounts
      const stream = video.srcObject as MediaStream;
      if (stream) {
        const tracks = stream.getTracks();
        tracks.forEach((track: any) => track.stop());
      }
      video.srcObject = null;
    };
  }, [videoRef, canvasRef]);

  return (
    <>
      <video
        ref={videoRef}
        width={QRCODE_WIDTH}
        height={QRCODE_HEIGHT}
        className="oui-hidden oui-rounded-2xl oui-bg-base-10"
      />
      <canvas
        ref={canvasRef}
        width={QRCODE_WIDTH * RATIO}
        height={QRCODE_HEIGHT * RATIO}
        style={{ width: QRCODE_WIDTH, height: QRCODE_HEIGHT }}
        className="oui-rounded-2xl oui-bg-base-10"
      />
    </>
  );
};
