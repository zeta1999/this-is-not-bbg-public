import React, { useRef, useEffect, useState } from "react";
import * as THREE from "three";
import { createSurfaceScene, buildSurfaceMesh, addWireframe, addAxisLabels, disposeSurface, SurfaceSceneContext } from "./three/surfaceUtils";
import { colors, fonts } from "../styles/theme";

export interface SwaptionCubeData {
  tenors: string[];      // swap tenors: ["1Y", "2Y", "5Y", "10Y", "30Y"]
  expiries: string[];    // option expiries: ["1M", "3M", "6M", "1Y", "2Y"]
  strikes: number[];     // moneyness or absolute
  vols: number[][][];    // vols[tenor_idx][expiry_idx][strike_idx]
  asset?: string;
}

interface Props {
  data: SwaptionCubeData;
}

export const SwaptionCube3D: React.FC<Props> = ({ data }) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const ctxRef = useRef<SurfaceSceneContext | null>(null);
  const [selectedTenor, setSelectedTenor] = useState(0);
  const [tooltip, setTooltip] = useState<{ x: number; y: number; text: string } | null>(null);

  useEffect(() => {
    const container = containerRef.current;
    if (!container || !data.vols || data.vols.length === 0) return;

    if (ctxRef.current) {
      disposeSurface(ctxRef.current);
      ctxRef.current = null;
    }

    const ctx = createSurfaceScene(container);
    ctxRef.current = ctx;

    // Get the 2D slice for the selected tenor.
    const tenorIdx = Math.min(selectedTenor, data.tenors.length - 1);
    const slice = data.vols[tenorIdx] || [];

    const colorScale = {
      minColor: new THREE.Color(0x0066ff),
      midColor: new THREE.Color(0xffff00),
      maxColor: new THREE.Color(0xff0000),
    };

    const mesh = buildSurfaceMesh(data.strikes, data.expiries.length, slice, colorScale);
    ctx.scene.add(mesh);
    addWireframe(ctx.scene, mesh);

    const nx = data.strikes.length;
    const ny = data.expiries.length;
    const xScale = 4 / (nx - 1 || 1);
    const xLabels = data.strikes.map((s) => s.toFixed(0) + "bp");
    addAxisLabels(ctx.scene, xLabels, data.expiries, xScale, 1, nx, ny);

    // Raycaster.
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
        const xi = Math.round((point.x / xScale) + nx / 2);
        const yi = Math.round((point.z / (4 / (ny - 1 || 1))) + ny / 2);
        if (xi >= 0 && xi < nx && yi >= 0 && yi < ny) {
          const vol = slice[yi]?.[xi];
          setTooltip({
            x: event.clientX - rect.left,
            y: event.clientY - rect.top,
            text: `Tenor: ${data.tenors[tenorIdx]} | Expiry: ${data.expiries[yi]} | Strike: ${data.strikes[xi]} | Vol: ${vol?.toFixed(1)}%`,
          });
        }
      } else {
        setTooltip(null);
      }
    }

    container.addEventListener("pointermove", onPointerMove);

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
  }, [JSON.stringify(data), selectedTenor]);

  return (
    <div style={{ position: "relative", width: "100%", height: "100%" }}>
      <div style={s.header}>
        {data.asset && <span style={{ color: colors.amber, fontWeight: 700 }}>{data.asset}</span>}
        <span style={{ color: colors.dimText }}> Swaption Cube</span>
        <span style={{ color: colors.dimText, marginLeft: 16 }}>Tenor:</span>
        {data.tenors.map((t, i) => (
          <span
            key={t}
            onClick={() => setSelectedTenor(i)}
            style={{
              ...s.tenorBtn,
              color: i === selectedTenor ? colors.amber : colors.dimText,
              borderBottom: i === selectedTenor ? `2px solid ${colors.amber}` : "2px solid transparent",
            }}
          >
            {t}
          </span>
        ))}
      </div>
      <div ref={containerRef} style={{ width: "100%", height: "calc(100% - 28px)" }} />
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
  header: {
    fontFamily: fonts.mono,
    fontSize: 11,
    padding: "4px 8px",
    height: 28,
    display: "flex",
    alignItems: "center",
    gap: 4,
  },
  tenorBtn: {
    cursor: "pointer",
    padding: "2px 6px",
    fontFamily: fonts.mono,
    fontSize: 11,
    fontWeight: 700,
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
