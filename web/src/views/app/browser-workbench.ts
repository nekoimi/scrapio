// Scheduling stays independent of Vue so it can be checked with an offline clock.
export interface WorkbenchClock {
	now: () => number;
	later: (callback: () => void, delay: number) => any;
	cancel: (timer: any) => void;
}
const clock: WorkbenchClock = { now: () => Date.now(), later: (fn, ms) => setTimeout(fn, ms), cancel: (timer) => clearTimeout(timer) };
export function wheelPixels(event: { deltaY: number; deltaMode: number; ctrlKey?: boolean; metaKey?: boolean; shiftKey?: boolean }, height: number) {
	if (event.ctrlKey || event.metaKey || event.shiftKey || !Number.isFinite(event.deltaY)) return 0;
	const distance = event.deltaY * (event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? height : 1);
	return Math.max(-1200, Math.min(1200, distance));
}
export function framePoint(x: number, y: number, rect: { left: number; top: number; width: number; height: number }, width: number, height: number) {
	if (!rect.width || !rect.height || !width || !height) return;
	const px = ((x - rect.left) / rect.width) * width,
		py = ((y - rect.top) / rect.height) * height;
	if (px < 0 || py < 0 || px >= width || py >= height) return;
	return { x: px, y: py };
}
export function createWheelQueue(options: { ready: () => boolean; dispatch: (distance: number) => Promise<boolean>; clock?: WorkbenchClock }) {
	const time = options.clock || clock;
	let distance = 0,
		timer: any,
		running = false,
		activeGeneration = 0,
		generation = 0,
		disposed = false;
	function clear() {
		generation++;
		distance = 0;
		time.cancel(timer);
		timer = undefined;
	}
	async function pump() {
		if (disposed || running || timer !== undefined || !distance || !options.ready()) return;
		const delta = Math.round(distance),
			epoch = generation;
		distance = 0;
		if (!delta) return;
		running = true;
		activeGeneration = epoch;
		try {
			if (!(await options.dispatch(delta)) && epoch === generation) clear();
		} catch {
			if (epoch === generation) clear();
		} finally {
			running = false;
			if (epoch === generation) void pump();
		}
	}
	function add(delta: number) {
		if (disposed || !Number.isFinite(delta) || (running ? activeGeneration !== generation : !options.ready())) return false;
		distance = Math.max(-1200, Math.min(1200, distance + delta));
		if (timer === undefined)
			timer = time.later(() => {
				timer = undefined;
				void pump();
			}, 120);
		return true;
	}
	return {
		add,
		pump,
		clear,
		get running() {
			return running;
		},
		dispose() {
			disposed = true;
			clear();
		},
	};
}
export function createAdaptiveRefresh(options: {
	allowed: () => boolean;
	capture: () => Promise<boolean>;
	clock?: WorkbenchClock;
	report?: (duration: number, delay: number, failures: number) => void;
}) {
	const time = options.clock || clock;
	let timer: any,
		running = false,
		disposed = false,
		activeUntil = 0,
		duration = 0,
		failures = 0;
	function delay() {
		return Math.min(15000, Math.max(time.now() < activeUntil ? 400 : 1800, duration) * 2 ** failures);
	}
	function schedule() {
		time.cancel(timer);
		timer = undefined;
		if (!disposed && !running && options.allowed())
			timer = time.later(() => {
				timer = undefined;
				void run();
			}, delay());
	}
	async function run() {
		if (disposed || running || !options.allowed()) return;
		running = true;
		const start = time.now();
		try {
			failures = (await options.capture()) ? 0 : Math.min(4, failures + 1);
		} catch {
			failures = Math.min(4, failures + 1);
		} finally {
			duration = Math.max(0, time.now() - start);
			running = false;
			if (!disposed) {
				options.report?.(duration, delay(), failures);
				schedule();
			}
		}
	}
	return {
		reconcile: schedule,
		activity() {
			activeUntil = time.now() + 2500;
			schedule();
		},
		dispose() {
			disposed = true;
			time.cancel(timer);
		},
	};
}
