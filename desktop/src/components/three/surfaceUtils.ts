import * as THREE from "three";
import { OrbitControls } from "three/addons/controls/OrbitControls.js";

export interface SurfaceSceneContext {
  renderer: THREE.WebGLRenderer;
  scene: THREE.Scene;
  camera: THREE.PerspectiveCamera;
  controls: OrbitControls;
  animationId: number;
}

/**
 * Creates a Three.js scene with camera, lights, and orbit controls.
 */
export function createSurfaceScene(container: HTMLDivElement): SurfaceSceneContext {
  const w = container.clientWidth || 600;
  const h = container.clientHeight || 400;

  const renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true });
  renderer.setSize(w, h);
  renderer.setPixelRatio(window.devicePixelRatio);
  renderer.setClearColor(0x0a0a0a);
  container.appendChild(renderer.domElement);

  const scene = new THREE.Scene();

  const camera = new THREE.PerspectiveCamera(45, w / h, 0.1, 1000);
  camera.position.set(4, 3, 5);
  camera.lookAt(0, 0, 0);

  // Lights.
  const ambient = new THREE.AmbientLight(0x404040, 2);
  scene.add(ambient);
  const directional = new THREE.DirectionalLight(0xffffff, 1.5);
  directional.position.set(5, 10, 7);
  scene.add(directional);

  // Controls.
  const controls = new OrbitControls(camera, renderer.domElement);
  controls.enableDamping = true;
  controls.dampingFactor = 0.05;

  // Animation loop.
  let animationId = 0;
  function animate() {
    animationId = requestAnimationFrame(animate);
    controls.update();
    renderer.render(scene, camera);
  }
  animate();

  return { renderer, scene, camera, controls, animationId };
}

/**
 * Builds a colored surface mesh from a 2D vol grid.
 * xLabels: moneyness values, yLabels: maturity indices, zValues: vol[y][x]
 */
export function buildSurfaceMesh(
  xValues: number[],
  yCount: number,
  zValues: number[][],
  colorScale: { minColor: THREE.Color; maxColor: THREE.Color; midColor?: THREE.Color }
): THREE.Mesh {
  const nx = xValues.length;
  const ny = yCount;

  // Find Z range for color mapping.
  let zMin = Infinity, zMax = -Infinity;
  for (let y = 0; y < ny; y++) {
    for (let x = 0; x < nx; x++) {
      const z = zValues[y]?.[x] ?? 0;
      if (z < zMin) zMin = z;
      if (z > zMax) zMax = z;
    }
  }
  const zRange = zMax - zMin || 1;

  // Build geometry.
  const geometry = new THREE.BufferGeometry();
  const vertices: number[] = [];
  const colors: number[] = [];
  const indices: number[] = [];

  // Scale factors to normalize the mesh to roughly [-2, 2] range.
  const xScale = 4 / (nx - 1 || 1);
  const yScale = 4 / (ny - 1 || 1);
  const zScale = 2 / zRange;

  for (let y = 0; y < ny; y++) {
    for (let x = 0; x < nx; x++) {
      const z = zValues[y]?.[x] ?? 0;
      const px = (x - nx / 2) * xScale;
      const py = (y - ny / 2) * yScale;
      const pz = (z - zMin) * zScale;
      vertices.push(px, pz, py); // Y-up in Three.js

      // Color: blue → yellow → red based on normalized Z.
      const t = (z - zMin) / zRange;
      const color = new THREE.Color();
      if (t < 0.5) {
        color.lerpColors(colorScale.minColor, colorScale.midColor || new THREE.Color(0xffff00), t * 2);
      } else {
        color.lerpColors(colorScale.midColor || new THREE.Color(0xffff00), colorScale.maxColor, (t - 0.5) * 2);
      }
      colors.push(color.r, color.g, color.b);
    }
  }

  // Build triangle indices.
  for (let y = 0; y < ny - 1; y++) {
    for (let x = 0; x < nx - 1; x++) {
      const a = y * nx + x;
      const b = a + 1;
      const c = (y + 1) * nx + x;
      const d = c + 1;
      indices.push(a, c, b);
      indices.push(b, c, d);
    }
  }

  geometry.setAttribute("position", new THREE.Float32BufferAttribute(vertices, 3));
  geometry.setAttribute("color", new THREE.Float32BufferAttribute(colors, 3));
  geometry.setIndex(indices);
  geometry.computeVertexNormals();

  const material = new THREE.MeshPhongMaterial({
    vertexColors: true,
    side: THREE.DoubleSide,
    shininess: 30,
  });

  return new THREE.Mesh(geometry, material);
}

/**
 * Adds a wireframe overlay to the surface for readability.
 */
export function addWireframe(scene: THREE.Scene, mesh: THREE.Mesh): THREE.LineSegments {
  const wireGeo = new THREE.WireframeGeometry(mesh.geometry);
  const wireMat = new THREE.LineBasicMaterial({ color: 0x333333, opacity: 0.3, transparent: true });
  const wire = new THREE.LineSegments(wireGeo, wireMat);
  scene.add(wire);
  return wire;
}

/**
 * Adds simple axis labels as sprites.
 */
export function addAxisLabels(
  scene: THREE.Scene,
  xLabels: string[],
  yLabels: string[],
  xScale: number,
  yScale: number,
  nx: number,
  ny: number
): void {
  // X axis labels (moneyness).
  for (let i = 0; i < xLabels.length; i += Math.max(1, Math.floor(xLabels.length / 5))) {
    const sprite = makeTextSprite(xLabels[i], { fontSize: 24, color: "#888888" });
    sprite.position.set((i - nx / 2) * xScale, -0.5, (ny / 2 + 0.5) * (4 / (ny - 1 || 1)));
    sprite.scale.set(0.5, 0.25, 1);
    scene.add(sprite);
  }
  // Y axis labels (maturity).
  for (let i = 0; i < yLabels.length; i++) {
    const sprite = makeTextSprite(yLabels[i], { fontSize: 24, color: "#888888" });
    sprite.position.set((-nx / 2 - 1) * xScale, -0.5, (i - ny / 2) * (4 / (ny - 1 || 1)));
    sprite.scale.set(0.5, 0.25, 1);
    scene.add(sprite);
  }
}

function makeTextSprite(text: string, opts: { fontSize: number; color: string }): THREE.Sprite {
  const canvas = document.createElement("canvas");
  canvas.width = 256;
  canvas.height = 64;
  const ctx = canvas.getContext("2d")!;
  ctx.font = `${opts.fontSize}px monospace`;
  ctx.fillStyle = opts.color;
  ctx.textAlign = "center";
  ctx.fillText(text, 128, 40);

  const texture = new THREE.CanvasTexture(canvas);
  const material = new THREE.SpriteMaterial({ map: texture, transparent: true });
  return new THREE.Sprite(material);
}

/**
 * Dispose all scene resources.
 */
export function disposeSurface(ctx: SurfaceSceneContext): void {
  cancelAnimationFrame(ctx.animationId);
  ctx.controls.dispose();
  ctx.renderer.dispose();
  ctx.scene.traverse((obj) => {
    if (obj instanceof THREE.Mesh) {
      obj.geometry.dispose();
      if (Array.isArray(obj.material)) {
        obj.material.forEach((m) => m.dispose());
      } else {
        obj.material.dispose();
      }
    }
  });
  ctx.renderer.domElement.remove();
}
