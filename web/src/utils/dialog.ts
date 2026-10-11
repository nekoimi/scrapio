import { ElMessageBox } from 'element-plus';

// Use the template's dialog styles and return an explicit decision. A second
// click while a dialog is open must not queue another destructive confirmation.
let active = false;

export async function confirmAction(message: string, title = '操作确认'): Promise<boolean> {
	if (active) return false;
	active = true;
	try {
		await ElMessageBox.confirm(message, title, {
			customClass: 'scrapio-dialog',
			confirmButtonText: '确认', cancelButtonText: '取消',
			closeOnClickModal: false, distinguishCancelAndClose: true,
		});
		return true;
	} catch (cause) {
		if (cause === 'cancel' || cause === 'close') return false;
		throw cause;
	} finally { active = false; }
}

export async function promptValue(message: string, initialValue = '', title = '填写信息'): Promise<string | null> {
	if (active) return null;
	active = true;
	try {
		const result = await ElMessageBox.prompt(message, title, {
			customClass: 'scrapio-dialog',
			inputValue: initialValue, confirmButtonText: '确认', cancelButtonText: '取消',
			closeOnClickModal: false, distinguishCancelAndClose: true,
			inputValidator: (value: string) => !!value?.trim() || '请输入内容',
		});
		if (typeof result === 'string') return null;
		return result.value.trim();
	} catch (cause) {
		if (cause === 'cancel' || cause === 'close') return null;
		throw cause;
	} finally { active = false; }
}
