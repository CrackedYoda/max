// Fake passcode/biometric checks: tests replace `fakePasscode.verify*` to hold them open.
export const fakePasscode = {
  verify: async () => ({ ok: true }),
  verifyBiometric: async () => 'ok',
};

export const verifyPasscode = (code) => fakePasscode.verify(code);
export const verifyBiometric = () => fakePasscode.verifyBiometric();
export const isBiometricEnabled = async () => false;
export const biometricLabel = async () => 'Face ID';
export const setPasscode = async () => {};
export const enableBiometric = async () => false;
