import { Redirect, useLocalSearchParams } from 'expo-router';

/**
 * max://verify/x — where MAX's X callback hands a native flow back. On iOS the
 * auth session captures it; Android may route it here, so pass it on.
 */
export default function VerifyXLink() {
  const { state, code, error } = useLocalSearchParams<{ state?: string; code?: string; error?: string }>();
  return <Redirect href={{ pathname: '/verify', params: { state: state ?? '', code: code ?? '', error: error ?? '' } }} />;
}
