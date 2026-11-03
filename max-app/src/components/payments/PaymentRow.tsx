import type { Payment, PaymentStatus } from '@max/sdk';
import { Linking, Pressable, View } from 'react-native';

import { AppText, Mono } from '@/components/ui/AppText';
import { Pill } from '@/components/ui/Blocks';
import { colors } from '@/theme';
import { relativeTime, shortAddress } from '@/utils/format';

const LABELS: Record<PaymentStatus, string> = {
  CREATED: 'Awaiting approval',
  QUOTED: 'Quoted',
  AWAITING_SENDER_SIGNATURE: 'Not signed',
  SUBMITTED: 'Submitted',
  CONFIRMING: 'Confirming',
  COMPLETED: 'Completed',
  EXPIRED: 'Expired',
  FAILED: 'Failed',
  REFUND_PENDING: 'Refund pending',
  REFUNDED: 'Refunded',
  MANUAL_REVIEW: 'Under review',
};

export const statusLabel = (s: PaymentStatus) => LABELS[s] ?? s;

export function StatusPill({ status }: { status: PaymentStatus }) {
  const tone = status === 'COMPLETED' ? 'success' : status === 'FAILED' || status === 'EXPIRED' ? 'danger' : status === 'MANUAL_REVIEW' || status === 'REFUND_PENDING' ? 'clay' : 'neutral';
  return <Pill small tone={tone} label={statusLabel(status)} />;
}

export function assetSymbol(p: Payment): string {
  return p.asset.symbol ?? (p.asset.kind === 'native' ? p.chain.nativeCoin : p.asset.address ? shortAddress(p.asset.address) : '');
}

/** Activity line from the Home artboard: ↓ From … / ↑ To …, amount on the right. */
export function PaymentRow({ payment, direction }: { payment: Payment; direction: 'sent' | 'received' }) {
  const out = direction === 'sent';
  const who = out ? payment.recipient.handle : shortAddress(payment.senderAddress);
  const settled = payment.status === 'COMPLETED';
  const amount = `${out ? '−' : '+'}${payment.amount.display} ${assetSymbol(payment)}`;
  const body = (
    <>
      <View style={{ width: 36, height: 36, borderRadius: 18, alignItems: 'center', justifyContent: 'center', backgroundColor: out ? colors.sunken : colors.successSoft }}>
        <AppText weight="semibold" size={16} color={out ? colors.body : colors.success}>
          {out ? '↑' : '↓'}
        </AppText>
      </View>
      <View style={{ flex: 1, minWidth: 0, gap: 2 }}>
        <AppText weight="medium" size={14} numberOfLines={1}>
          {out ? 'To ' : 'From '}
          <Mono size={14}>{who}</Mono>
        </AppText>
        <AppText variant="caption" numberOfLines={1}>
          {payment.chain.name} · {relativeTime(payment.createdAt)}
        </AppText>
      </View>
      <View style={{ alignItems: 'flex-end', gap: 4 }}>
        <AppText weight="semibold" size={14} color={out ? colors.ink : colors.success}>
          {amount}
        </AppText>
        {settled ? null : <StatusPill status={payment.status} />}
      </View>
    </>
  );
  const style = { flexDirection: 'row' as const, alignItems: 'center' as const, gap: 12, paddingVertical: 10, paddingHorizontal: 4 };
  const label = `${out ? 'Sent' : 'Received'} ${amount} ${out ? 'to' : 'from'} ${who} on ${payment.chain.name}, ${statusLabel(payment.status)}`;
  if (!payment.explorerUrl) {
    return (
      <View accessible accessibilityLabel={label} style={style}>
        {body}
      </View>
    );
  }
  return (
    <Pressable accessibilityRole="link" accessibilityLabel={label} accessibilityHint="Opens the block explorer" onPress={() => Linking.openURL(payment.explorerUrl!)} style={({ pressed }) => [style, pressed && { opacity: 0.7 }]}>
      {body}
    </Pressable>
  );
}
