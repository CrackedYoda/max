import { Component, type ReactNode } from 'react';
import { View } from 'react-native';

import { AppText } from '@/components/ui/AppText';
import { Button } from '@/components/ui/Button';
import { colors } from '@/theme';

type State = { error: Error | null };

/** Last-resort screen. Never shows stack traces or key material. */
export class ErrorBoundary extends Component<{ children: ReactNode }, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error) {
    console.warn('Render error:', error.name);
  }

  render() {
    if (!this.state.error) return this.props.children;
    return (
      <View style={{ flex: 1, backgroundColor: colors.ivory, padding: 24, justifyContent: 'center', gap: 16 }}>
        <AppText variant="h1">Something went wrong</AppText>
        <AppText variant="lead">Your wallet and funds are safe. Try again; if it keeps happening, restart the app.</AppText>
        <Button label="Try again" onPress={() => this.setState({ error: null })} />
      </View>
    );
  }
}
