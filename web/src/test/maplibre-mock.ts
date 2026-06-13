import { vi } from 'vitest';

// Minimal maplibre-gl stand-in implementing exactly the surface the map
// components (TripMap/TrackerMap) use. Named type exports (Map/Marker/
// GeoJSONSource/StyleSpecification/LngLatBoundsLike) are erased by tsc, so only
// the runtime `default` matters.

export interface FakeSource {
  setData: ReturnType<typeof vi.fn>;
}

export class FakeMap {
  static instances: FakeMap[] = [];
  opts: unknown;
  handlers: Record<string, Array<() => void>> = {};
  // Layer-scoped handlers (map.on('click', layerId, cb)), keyed "evt:layer".
  layerHandlers: Record<string, Array<(e: unknown) => void>> = {};
  onceHandlers: Record<string, Array<() => void>> = {};
  sources = new Map<string, FakeSource>();
  layers: unknown[] = [];
  controls: unknown[] = [];
  controlPositions = new Map<unknown, string | undefined>();
  styleLoaded = true;
  addControl = vi.fn((ctrl: unknown, position?: string) => {
    this.controls.push(ctrl);
    this.controlPositions.set(ctrl, position);
  });
  removeControl = vi.fn((ctrl: unknown) => {
    this.controls = this.controls.filter((c) => c !== ctrl);
    this.controlPositions.delete(ctrl);
  });
  fitBounds = vi.fn();
  flyTo = vi.fn();
  remove = vi.fn();
  // A stable canvas so cursor writes persist across getCanvas() calls.
  canvasEl = { style: {} as CSSStyleDeclaration };
  getCanvas = vi.fn(() => this.canvasEl);

  constructor(opts: unknown) {
    this.opts = opts;
    FakeMap.instances.push(this);
  }

  // Overloaded: on(evt, cb) for map events, on(evt, layer, cb) for layer events.
  on(evt: string, a: ((e?: unknown) => void) | string, b?: (e: unknown) => void): void {
    if (typeof a === 'function') {
      (this.handlers[evt] ??= []).push(a as () => void);
      // Fire 'load' immediately so addSource/addLayer code runs deterministically.
      if (evt === 'load') (a as () => void)();
      return;
    }
    (this.layerHandlers[`${evt}:${a}`] ??= []).push(b!);
  }

  /** Test helper: simulate a feature click/hover on a layer. */
  fireLayer(evt: string, layer: string, e: unknown): void {
    for (const cb of this.layerHandlers[`${evt}:${layer}`] ?? []) cb(e);
  }

  once(evt: string, cb: () => void): void {
    (this.onceHandlers[evt] ??= []).push(cb);
    // Fire 'load'/'idle' immediately so the deferred apply()/fit() paths run.
    if (evt === 'load' || evt === 'idle') cb();
  }

  fire(evt: string): void {
    for (const cb of this.handlers[evt] ?? []) cb();
  }

  addSource(id: string, _spec: unknown): void {
    this.sources.set(id, { setData: vi.fn() });
  }

  addLayer(spec: unknown): void {
    this.layers.push(spec);
  }

  getSource(id: string): FakeSource | undefined {
    return this.sources.get(id);
  }

  isStyleLoaded(): boolean {
    return this.styleLoaded;
  }
}

export class FakeMarker {
  static instances: FakeMarker[] = [];
  opts: { element?: HTMLElement } | undefined;
  lngLat: [number, number] | null = null;
  rotation = 0;
  added = false;
  popup: FakePopup | null = null;
  remove = vi.fn();

  constructor(opts?: { element?: HTMLElement }) {
    this.opts = opts;
    FakeMarker.instances.push(this);
  }

  setLngLat(ll: [number, number]): this {
    this.lngLat = ll;
    return this;
  }

  setPopup(p: FakePopup): this {
    this.popup = p;
    return this;
  }

  setRotation(r: number): this {
    this.rotation = r;
    return this;
  }

  addTo(): this {
    this.added = true;
    return this;
  }

  getElement(): HTMLElement {
    return this.opts?.element as HTMLElement;
  }
}

export class FakePopup {
  static instances: FakePopup[] = [];
  opts: unknown;
  lngLat: [number, number] | null = null;
  html = '';
  added = false;
  remove = vi.fn(() => {
    this.added = false;
  });

  constructor(opts?: unknown) {
    this.opts = opts;
    FakePopup.instances.push(this);
  }

  setLngLat(ll: [number, number]): this {
    this.lngLat = ll;
    return this;
  }

  setHTML(h: string): this {
    this.html = h;
    return this;
  }

  setDOMContent(node: Node): this {
    this.html = (node as HTMLElement).textContent ?? '';
    return this;
  }

  addTo(): this {
    this.added = true;
    return this;
  }

  isOpen(): boolean {
    return this.added;
  }
}

export class FakeNavigationControl {}

export class FakeAttributionControl {
  constructor(public opts?: unknown) {}
}

export function resetMaplibreMock(): void {
  FakeMap.instances = [];
  FakeMarker.instances = [];
  FakePopup.instances = [];
}

const maplibregl = {
  Map: FakeMap,
  Marker: FakeMarker,
  Popup: FakePopup,
  NavigationControl: FakeNavigationControl,
  AttributionControl: FakeAttributionControl,
};

export default maplibregl;
