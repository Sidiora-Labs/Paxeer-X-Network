import { Suspense, useEffect, useMemo, useRef, useState } from "react";
import { Canvas, useFrame } from "@react-three/fiber";
import { Center, useGLTF } from "@react-three/drei";
import { Box3, Vector3, type Group } from "three";

const MODEL_URL = "/Model_Compressed.glb";
// Target size (in world units) that the model's largest axis should occupy.
// Combined with the Canvas camera settings below, this keeps the model
// comfortably inside the visible frustum regardless of the GLTF's own scale.
const TARGET_SIZE = 2;

// Preload the model so the first mount doesn't cold-start the fetch.
// Safe to call during module eval — drei guards for window internally.
if (typeof window !== "undefined") {
  try { (useGLTF as any).preload(MODEL_URL); } catch {}
}

function SpinningModel({ speed = 0.6 }: { speed?: number }) {
  const { scene } = useGLTF(MODEL_URL);
  const rotRef = useRef<Group>(null);

  // Clone so multiple concurrent spinners don't share mutations.
  const cloned = useMemo(() => scene.clone(true), [scene]);

  // Measure the model's bounding box once it's loaded and derive a uniform
  // scale so its largest dimension == TARGET_SIZE. <Center> then recenters
  // the scaled geometry at the origin so it spins in place.
  const fitScale = useMemo(() => {
    const box = new Box3().setFromObject(cloned);
    const size = box.getSize(new Vector3());
    const maxDim = Math.max(size.x, size.y, size.z);
    return maxDim > 0 ? TARGET_SIZE / maxDim : 1;
  }, [cloned]);

  useFrame((_, delta) => {
    if (rotRef.current) rotRef.current.rotation.y += delta * speed;
  });

  return (
    <group ref={rotRef}>
      <Center>
        <primitive object={cloned} scale={fitScale} />
      </Center>
    </group>
  );
}

interface PaxSpinnerProps {
  className?: string;
  style?: React.CSSProperties;
  speed?: number;
  cameraZ?: number;
  active?: boolean; // when false, pauses the render loop to save CPU
}

/**
 * Rendered 3D spinner using the Pax model. Client-only (skips SSR).
 */
export function PaxSpinner({
  className,
  style,
  speed = 0.6,
  cameraZ = 4,
  active = true,
}: PaxSpinnerProps) {
  const [mounted, setMounted] = useState(false);
  useEffect(() => { setMounted(true); }, []);
  if (!mounted) return null;

  return (
    <div className={className} style={style}>
      <Canvas
        camera={{ position: [0, 0, cameraZ], fov: 40 }}
        gl={{ antialias: true, alpha: true, powerPreference: "low-power" }}
        dpr={[1, 1.5]}
        frameloop={active ? "always" : "never"}
        style={{ background: "transparent" }}
      >
        <ambientLight intensity={0.9} />
        <directionalLight position={[3, 4, 5]} intensity={1.2} />
        <directionalLight position={[-3, -2, -3]} intensity={0.5} />
        <Suspense fallback={null}>
          <SpinningModel speed={speed} />
        </Suspense>
      </Canvas>
    </div>
  );
}
