import { validBrowserState, type BrowserState, type CaptureBlob } from './browser-state';
import { computed, nextTick, shallowRef, ref } from 'vue';

export interface FrameTarget {
	sessionID: string;
	pageStateID: string;
	url: string;
	available: boolean;
	state?: BrowserState;
}
interface DecodedFrame extends FrameTarget {
	objectURL: string;
	width: number;
	height: number;
}
interface FrameOptions {
	load: (target: FrameTarget) => Promise<Blob>;
	decode?: (url: string) => Promise<{ width: number; height: number }>;
	createURL?: (blob: Blob) => string;
	revokeURL?: (url: string) => void;
}

async function decodeImage(url: string) {
	const image = new Image();
	let timer: ReturnType<typeof setTimeout> | undefined;
	try {
		image.src = url;
		await Promise.race([
			image.decode(),
			new Promise<never>((_, reject) => {
				timer = setTimeout(() => reject(new Error('画面解码超时')), 5000);
			}),
		]);
		if (!image.naturalWidth || !image.naturalHeight) throw new Error('画面尺寸无效');
		return { width: image.naturalWidth, height: image.naturalHeight };
	} finally {
		if (timer) clearTimeout(timer);
		image.src = '';
	}
}

// One in-flight capture plus one replaceable demand, including image decoding.
// Keep the previous image until its replacement is decoded and rendered.
export function createBrowserFrame(options: FrameOptions) {
	const frame = shallowRef<DecodedFrame>();
	const target = shallowRef<FrameTarget>();
	const busy = ref(false),
		error = ref(''),
		needsRefresh = ref(true);
	const decode = options.decode || decodeImage;
	const createURL = options.createURL || ((blob: Blob) => URL.createObjectURL(blob));
	const revokeURL = options.revokeURL || ((url: string) => URL.revokeObjectURL(url));
	let generation = 0,
		sequence = 0,
		disposed = false;
	let pending: { target: FrameTarget; generation: number; sequence: number } | undefined;
	let activeJob: typeof pending;
	let drain: Promise<void> | undefined;
	const stale = computed(
		() =>
			needsRefresh.value ||
			!target.value?.available ||
			!frame.value ||
			frame.value.sessionID !== target.value.sessionID ||
			frame.value.pageStateID !== target.value.pageStateID
	);
	function invalidate() {
		generation++;
		pending = undefined;
		needsRefresh.value = true;
	}
	function clear() {
		invalidate();
		if (frame.value) revokeURL(frame.value.objectURL);
		frame.value = undefined;
		error.value = '';
	}
	function setTarget(value?: FrameTarget) {
		if (disposed) return;
		const prior = target.value;
		if (prior?.sessionID !== value?.sessionID) clear();
		else if (
			prior?.pageStateID !== value?.pageStateID ||
			prior?.available !== value?.available ||
			prior?.url !== value?.url ||
			prior?.state?.document_id !== value?.state?.document_id ||
			prior?.state?.frame_state_id !== value?.state?.frame_state_id ||
			prior?.state?.validation_state_id !== value?.state?.validation_state_id
		) {
			invalidate();
			error.value = '';
		}
		target.value = value ? { ...value } : undefined;
	}
	function valid(job: NonNullable<typeof pending>) {
		return (
			!disposed &&
			generation === job.generation &&
			sequence === job.sequence &&
			target.value?.available &&
			target.value.sessionID === job.target.sessionID &&
			target.value.pageStateID === job.target.pageStateID
		);
	}
	async function run() {
		// A changing page can invalidate a capture repeatedly. Never loop forever.
		for (let attempt = 0; pending && attempt < 3; attempt++) {
			const job = pending;
			pending = undefined;
			activeJob = job;
			let objectURL = '';
			try {
				const blob = (await options.load(job.target)) as CaptureBlob;
				const metadata = blob.frameState;
				if (job.target.state && !metadata) throw new Error('缺少画面元数据');
				if (
					metadata &&
					(!validBrowserState(metadata, job.target.pageStateID, true) ||
						(job.target.state &&
							(metadata.document_id !== job.target.state.document_id ||
								metadata.frame_state_id !== job.target.state.frame_state_id ||
								metadata.validation_state_id !== job.target.state.validation_state_id)))
				)
					throw new Error('画面版本不一致');
				if (!valid(job)) continue;
				objectURL = createURL(blob);
				const size = await decode(objectURL);
				if (!valid(job)) continue;
				if (
					metadata &&
					(metadata.image!.width !== size.width ||
						metadata.image!.height !== size.height ||
						(frame.value?.sessionID === job.target.sessionID &&
							frame.value.state?.frame_sequence &&
							metadata.frame_sequence! <= frame.value.state.frame_sequence))
				)
					throw new Error('画面尺寸或帧序号无效');
				const prior = frame.value;
				frame.value = { ...job.target, state: metadata, objectURL, ...size };
				objectURL = ''; // Ownership transferred to the displayed frame.
				needsRefresh.value = false;
				error.value = '';
				await nextTick();
				if (prior) revokeURL(prior.objectURL);
			} catch (cause: any) {
				if (!valid(job)) continue;
				needsRefresh.value = true;
				error.value =
					cause?.error?.code === 'STALE_PAGE_STATE' ? '页面已变化，请更新画面后再点选。' : '画面更新失败，保留上次截图；请确认连接后更新画面。';
			} finally {
				if (activeJob === job) activeJob = undefined;
				if (objectURL) revokeURL(objectURL);
			}
		}
		if (pending) {
			pending = undefined;
			needsRefresh.value = true;
			error.value = '页面持续更新，暂时无法点选；请稍后更新画面。';
		}
	}
	function startDrain(): Promise<void> {
		busy.value = true;
		const task = Promise.resolve()
			.then(run)
			.finally(() => {
				if (drain === task) drain = undefined;
				// A demand can arrive after run finishes but before its promise settles.
				// Continue it without exposing an idle/interactive gap.
				if (pending && !disposed) return startDrain();
				busy.value = false;
			});
		drain = task;
		return task;
	}
	function refresh(): Promise<void> {
		if (disposed || !target.value?.available || !target.value.url) return Promise.resolve();
		// A same-version caller can share capture/decode already in progress.
		// Changed targets and explicit invalidation still demand a new frame.
		const job = pending || activeJob;
		if (drain && job?.generation === generation && job.target.sessionID === target.value.sessionID && job.target.pageStateID === target.value.pageStateID) return drain;
		needsRefresh.value = true;
		error.value = '';
		pending = { target: { ...target.value }, generation, sequence: ++sequence };
		return drain || startDrain();
	}
	function dispose() {
		disposed = true;
		clear();
		target.value = undefined;
	}
	return { frame, busy, error, stale, setTarget, refresh, invalidate, clear, dispose };
}
