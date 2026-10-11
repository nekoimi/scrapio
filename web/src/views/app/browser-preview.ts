import { nextTick, shallowRef } from 'vue';

export interface PreviewMetadata {
	protocol: 'editor.preview.v1';
	sequence: number;
	mime: 'image/jpeg';
	width: number;
	height: number;
	captured_at_unix_ms: number;
	preview_only: true;
}
export interface PreviewPacket {
	metadata: PreviewMetadata;
	image: Blob;
}

// Framing tolerates arbitrary HTTP chunk boundaries and rejects unbounded packets.
export function createPreviewDecoder() {
	let buffer = new Uint8Array(0),
		sequence = 0;
	return {
		feed(chunk: Uint8Array): PreviewPacket[] {
			if (buffer.length + chunk.length > 2 * (4 * 1024 * 1024 + 4096 + 8)) throw new Error('流式画面缓冲超出限制');
			const joined = new Uint8Array(buffer.length + chunk.length);
			joined.set(buffer);
			joined.set(chunk, buffer.length);
			buffer = joined;
			const packets: PreviewPacket[] = [];
			while (buffer.length >= 8) {
				const sizes = new DataView(buffer.buffer, buffer.byteOffset, 8),
					headerSize = sizes.getUint32(0),
					imageSize = sizes.getUint32(4);
				if (!headerSize || headerSize > 4096 || imageSize > 4 * 1024 * 1024) throw new Error('流式画面包超出限制');
				const total = 8 + headerSize + imageSize;
				if (buffer.length < total) break;
				const metadata = JSON.parse(new TextDecoder().decode(buffer.subarray(8, 8 + headerSize)));
				if (metadata.heartbeat === true && metadata.protocol === 'editor.preview.v1' && imageSize === 0) {
					buffer = buffer.slice(total);
					continue;
				}
				if (metadata.error_code) throw Object.assign(new Error('流式画面暂不可用'), { code: metadata.error_code });
				if (
					metadata.protocol !== 'editor.preview.v1' ||
					!metadata.preview_only ||
					metadata.mime !== 'image/jpeg' ||
					!Number.isSafeInteger(metadata.sequence) ||
					metadata.sequence <= sequence ||
					!imageSize ||
					!Number.isInteger(metadata.width) ||
					!Number.isInteger(metadata.height) ||
					metadata.width < 1 ||
					metadata.width > 1920 ||
					metadata.height < 1 ||
					metadata.height > 1080 ||
					!Number.isSafeInteger(metadata.captured_at_unix_ms) ||
					metadata.captured_at_unix_ms < 1
				)
					throw new Error('流式画面元数据无效');
				sequence = metadata.sequence;
				const bytes = buffer.slice(8 + headerSize, total);
				packets.push({ metadata, image: new Blob([bytes], { type: 'image/jpeg' }) });
				buffer = buffer.slice(total);
			}
			return packets;
		},
		finish() {
			if (buffer.length) throw new Error('流式画面包不完整');
		},
	};
}

// One decoder and one replaceable latest frame; slow rendering never builds a video backlog.
export function createPreviewDisplay(
	options: {
		decode?: (url: string) => Promise<{ width: number; height: number }>;
		createURL?: (blob: Blob) => string;
		revokeURL?: (url: string) => void;
		failure?: () => void;
	} = {}
) {
	const frame = shallowRef<PreviewPacket & { objectURL: string }>();
	const createURL = options.createURL || ((blob) => URL.createObjectURL(blob));
	const revokeURL = options.revokeURL || ((url) => URL.revokeObjectURL(url));
	const decode =
		options.decode ||
		(async (url) => {
			const image = new Image();
			image.src = url;
			let timer: ReturnType<typeof setTimeout> | undefined;
			try {
				await Promise.race([
					image.decode(),
					new Promise<never>((_, reject) => {
						timer = setTimeout(() => reject(new Error('preview decode timeout')), 3000);
					}),
				]);
				return { width: image.naturalWidth, height: image.naturalHeight };
			} finally {
				clearTimeout(timer);
				image.src = '';
			}
		});
	let pending: PreviewPacket | undefined,
		running = false,
		generation = 0;
	async function pump() {
		if (running) return;
		running = true;
		try {
			while (pending) {
				const packet = pending,
					epoch = generation;
				pending = undefined;
				const url = createURL(packet.image);
				try {
					const size = await decode(url);
					if (epoch !== generation) {
						revokeURL(url);
						continue;
					}
					if (size.width !== packet.metadata.width || size.height !== packet.metadata.height) throw new Error('preview dimensions mismatch');
					const prior = frame.value;
					frame.value = { ...packet, objectURL: url };
					await nextTick();
					if (prior) revokeURL(prior.objectURL);
				} catch {
					revokeURL(url);
					if (epoch === generation) options.failure?.();
				}
			}
		} finally {
			running = false;
		}
	}
	return {
		frame,
		push(packet: PreviewPacket) {
			pending = packet;
			void pump();
		},
		clear() {
			generation++;
			pending = undefined;
			if (frame.value) revokeURL(frame.value.objectURL);
			frame.value = undefined;
		},
	};
}
