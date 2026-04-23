import React, { useRef, useEffect, useState } from "react";
import * as THREE from "three";
import { createSurfaceScene, buildSurfaceMesh, addWireframe, addAxisLabels, disposeSurface, SurfaceSceneContext } from "./three/surfaceUtils";
import { colors, fonts } from "../styles/theme";

export interface VolSurfaceData {
  maturities: string[];
  moneyness: number[];
  vols: number[][]; // vols[maturity_idx][moneyness_idx] in percent
  spot?: number;
  asset?: string;
}

interface Props {
  data: VolSurfaceData;
}

export const VolSurface3D: React.FC<Props> = ({ data }) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const ctxRef = useRef<SurfaceSceneContext | null>(null);
  const [tooltip, setTooltip] = useState<{ x: number; y: number; text: string } | null>(null);

  useEffect(() => {
    const container = containerRef.current;
    if (!container || !data.vols || data.vols.length === 0) return;

    // Cleanup previous scene.
    if (ctxRef.current) {
      disposeSurface(ctxRef.current);
      ctxRef.current = null;
    }

    const ctx = createSurfaceScene(container);
    ctxRef.current = ctx;

    // Build surface mesh.
    const colorScale = {
      minColor: new THREE.Color(0x0066ff),
      midColor: new THREE.Color(0xffff00),
      maxColor: new THREE.Color(0xff0000),
    };

    const mesh = buildSurfaceMesh(data.moneyness, data.maturities.length, data.vols, colorScale);
    ctx.scene.add(mesh);
    addWireframe(ctx.scene, mesh);

    // Axis labels.
    const nx = data.moneyness.length;
    const ny = data.maturities.length;
    const xScale = 4 / (nx - 1 || 1);
    const xLabels = data.moneyness.map((m) => (m * 100).toFixed(0) + "%");
    addAxisLabels(ctx.scene, xLabels, data.maturities, xScale, 1, nx, ny);

    // Raycaster for tooltip.
    const raycaster = new THREE.Raycaster();
    const mouse = new THREE.Vector2();

    function onPointerMove(event: PointerEvent) {
      const rect = container!.getBoundingClientRect();
      mouse.x = ((event.clientX - rect.left) / rect.width) * 2 - 1;
      mouse.y = -((event.clientY - rect.top) / rect.height) * 2 + 1;

      raycaster.setFromCamera(mouse, ctx.camera);
      const intersects = raycaster.intersectObject(mesh);

      if (intersects.length > 0) {
        const point = intersects[0].point;
        // Reverse-map position to data indices.
        const xi = Math.round((point.x / xScale) + nx / 2);
        const yi = Math.round((point.z / (4 / (ny - 1 || 1))) + ny / 2);
        if (xi >= 0 && xi < nx && yi >= 0 && yi < ny) {
          const vol = data.vols[yi]?.[xi];
          const maturity = data.maturities[yi] || "?";
          const moneyness = data.moneyness[xi] || 0;
          setTooltip({
            x: event.clientX - rect.left,
            y: event.clientY - rect.top,
            text: `${maturity} | ${(moneyness * 100).toFixed(0)}% | ${vol?.toFixed(1)}%`,
          });
        }
      } else {
        setTooltip(null);
      }
    }

    container.addEventListener("pointermove", onPointerMove);

    // Resize handler.
    const observer = new ResizeObserver(() => {
      const w = container.clientWidth;
      const h = container.clientHeight;
      ctx.camera.aspect = w / h;
      ctx.camera.updateProjectionMatrix();
      ctx.renderer.setSize(w, h);
    });
    observer.observe(container);

    return () => {
      container.removeEventListener("pointermove", onPointerMove);
      observer.disconnect();
      if (ctxRef.current) {
        disposeSurface(ctxRef.current);
        ctxRef.current = null;
      }
    };
  }, [JSON.stringify(data)]);

  return (
    <div style={{ position: "relative", width: "100%", height: "100%" }}>
      <div style={s.label}>
        {data.asset && <span style={{ color: colors.amber, fontWeight: 700 }}>{data.asset}</span>}
        <span style={{ color: colors.dimText }}> Vol Surface</span>
        {data.spot && <span style={{ color: colors.dimText }}> | Spot: {data.spot.toFixed(2)}</span>}
      </div>
      <div ref={containerRef} style={{ width: "100%", height: "calc(100% - 24px)" }} />
      {tooltip && (
        <div style={{
          ...s.tooltip,
          left: tooltip.x + 12,
          top: tooltip.y - 30,
        }}>
          {tooltip.text}
        </div>
      )}
    </div>
  );
};

const s: Record<string, React.CSSProperties> = {
  label: {
    fontFamily: fonts.mono,
    fontSize: 11,
    padding: "4px 8px",
    height: 24,
    display: "flex",
    alignItems: "center",
    gap: 8,
  },
  tooltip: {
    position: "absolute",
    background: "rgba(0,0,0,0.85)",
    color: colors.white,
    fontFamily: fonts.mono,
    fontSize: 11,
    padding: "4px 8px",
    borderRadius: 4,
    border: `1px solid ${colors.border}`,
    pointerEvents: "none",
    whiteSpace: "nowrap",
    zIndex: 100,
  },
};
