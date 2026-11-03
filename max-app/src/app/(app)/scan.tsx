import { CameraView, useCameraPermissions } from 'expo-camera';
import { router } from 'expo-router';
import { useRef, useState } from 'react';
import { StyleSheet, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { AppText } from '@/components/ui/AppText';
import { Button, IconButton } from '@/components/ui/Button';
import { colors } from '@/theme';
import { handleFromScan } from '@/validation/identity';

/** Scans a MAX profile QR and opens Send with that handle filled in. Nothing is paid from here. */
export default function Scan() {
  const insets = useSafeAreaInsets();
  const [permission, requestPermission] = useCameraPermissions();
  const [error, setError] = useState<string | null>(null);
  const handled = useRef(false);

  return (
    <View style={{ flex: 1, backgroundColor: colors.night }}>
      {permission?.granted ? (
        <CameraView
          style={StyleSheet.absoluteFill}
          facing="back"
          barcodeScannerSettings={{ barcodeTypes: ['qr'] }}
          onBarcodeScanned={({ data }) => {
            if (handled.current) return;
            const handle = handleFromScan(data);
            if (!handle) {
              setError('That QR code isn’t a MAX handle.');
              return;
            }
            handled.current = true;
            router.back();
            router.navigate({ pathname: '/send', params: { to: handle } });
          }}
        />
      ) : null}

      <View style={{ paddingTop: insets.top + 8, paddingHorizontal: 12, flexDirection: 'row' }}>
        <View style={{ backgroundColor: colors.surface, borderRadius: 12 }}>
          <IconButton icon="close" label="Close scanner" onPress={() => router.back()} />
        </View>
      </View>

      <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', padding: 24, gap: 16 }}>
        {permission && !permission.granted ? (
          <>
            <AppText variant="h1" color={colors.nightText} style={{ textAlign: 'center' }}>
              Scan a handle
            </AppText>
            <AppText variant="lead" color={colors.nightMuted} style={{ textAlign: 'center' }}>
              MAX uses the camera only to read payment QR codes.
            </AppText>
            <Button label={permission.canAskAgain ? 'Allow camera' : 'Camera is off in Settings'} disabled={!permission.canAskAgain} onPress={requestPermission} style={{ alignSelf: 'stretch' }} />
          </>
        ) : (
          <View style={{ width: 240, height: 240, borderRadius: 24, borderWidth: 3, borderColor: colors.nightAccent }} accessibilityLabel="Point the camera at a MAX QR code" />
        )}
      </View>

      <View style={{ paddingBottom: insets.bottom + 24, paddingHorizontal: 24, minHeight: 60 }}>
        {error ? (
          <AppText accessibilityRole="alert" weight="semibold" color={colors.nightText} style={{ textAlign: 'center' }}>
            {error}
          </AppText>
        ) : (
          <AppText color={colors.nightMuted} style={{ textAlign: 'center' }}>
            Point at a MAX profile QR code.
          </AppText>
        )}
      </View>
    </View>
  );
}
