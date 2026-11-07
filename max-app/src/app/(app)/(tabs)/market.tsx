import { isMaxApiError, type ListingCard } from '@max/sdk';
import { router } from 'expo-router';
import { useState } from 'react';
import { FlatList, Pressable, RefreshControl, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { AppText, Handle } from '@/components/ui/AppText';
import { Banner, Loading, Segmented } from '@/components/ui/Blocks';
import { Button } from '@/components/ui/Button';
import { TextField } from '@/components/ui/Fields';
import { Icon } from '@/components/ui/Icon';
import { marketErrorMessage } from '@/error/market';
import { useMarketSearch } from '@/hooks/queries';
import { useDebounced } from '@/hooks/useDebounced';
import { useSession } from '@/store/session';
import { colors, spacing } from '@/theme';
import { relativeTime } from '@/utils/format';
import { displayDecimal } from '@/utils/money';
import { stripPrefix } from '@/validation/identity';

type Sort = 'newest' | 'price_asc' | 'price_desc';

/** Market tab: search and browse active listings. Browsing needs no wallet session. */
export default function Market() {
  const insets = useSafeAreaInsets();
  const maxStatus = useSession((s) => s.max);
  const [q, setQ] = useState('');
  const [sort, setSort] = useState<Sort>('newest');
  const debounced = useDebounced(q.trim().toLowerCase());
  const search = useMarketSearch(debounced, sort);
  const listings = search.data?.pages.flatMap((p) => p.listings) ?? [];
  const disabled = isMaxApiError(search.error, 'MARKET_DISABLED') || (isMaxApiError(search.error) && search.error.status === 404);

  const header = (
    <View style={{ gap: 16, paddingBottom: 8 }}>
      <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
        <AppText variant="h2" accessibilityRole="header">
          Market
        </AppText>
        <Button label="My market" variant="soft" size="sm" disabled={maxStatus !== 'ready'} onPress={() => router.push('/market/offers')} />
      </View>
      <AppText variant="small">Buy and sell MAX IDs. Sales settle in USDC on Solana; MAX’s fee comes out of the seller’s proceeds.</AppText>
      {disabled ? null : (
        <>
          <TextField placeholder="Search names" value={q} onChangeText={(v) => setQ(stripPrefix(v))} returnKeyType="search" accessibilityLabel="Search names" />
          <Segmented
            label="Sort"
            value={sort}
            onChange={setSort}
            options={[
              { value: 'newest', label: 'Newest' },
              { value: 'price_asc', label: 'Price ↑' },
              { value: 'price_desc', label: 'Price ↓' },
            ]}
          />
        </>
      )}
      {disabled ? (
        <Banner tone="clay" title="The marketplace isn’t open yet">
          Check back soon.
        </Banner>
      ) : search.error ? (
        <Banner tone="danger">{marketErrorMessage(search.error)}</Banner>
      ) : null}
      {search.isLoading ? <Loading label="Loading listings" /> : null}
    </View>
  );

  return (
    <FlatList
      style={{ flex: 1, backgroundColor: colors.ivory }}
      contentContainerStyle={{ paddingTop: insets.top + 16, paddingHorizontal: spacing.gutter, paddingBottom: 24, gap: 10 }}
      data={disabled ? [] : listings}
      keyExtractor={(l) => l.listingId}
      keyboardShouldPersistTaps="handled"
      ListHeaderComponent={header}
      renderItem={({ item }) => <ListingRow listing={item} />}
      ListEmptyComponent={
        !disabled && search.isSuccess ? (
          <AppText variant="small" style={{ textAlign: 'center', paddingVertical: 24 }}>
            {debounced ? `No listings match “${debounced}”.` : 'Nothing is listed right now.'}
          </AppText>
        ) : undefined
      }
      ListFooterComponent={
        search.hasNextPage ? (
          <Button label="Load more" variant="secondary" size="md" loading={search.isFetchingNextPage} onPress={() => void search.fetchNextPage()} />
        ) : undefined
      }
      refreshControl={<RefreshControl refreshing={search.isRefetching && !search.isFetchingNextPage} onRefresh={() => void search.refetch()} tintColor={colors.muted} />}
    />
  );
}

function ListingRow({ listing: l }: { listing: ListingCard }) {
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`${l.handle}, ${l.price} ${l.symbol}. Open`}
      onPress={() => router.push({ pathname: '/market/[name]', params: { name: l.handle.replace(/^max\./, '') } })}
      style={({ pressed }) => ({
        flexDirection: 'row',
        alignItems: 'center',
        gap: 12,
        padding: 16,
        borderRadius: 18,
        borderWidth: 1,
        borderColor: colors.line,
        backgroundColor: pressed ? colors.sunken : colors.surface,
      })}>
      <View style={{ flex: 1, gap: 4 }}>
        <Handle name={l.handle} size={17} />
        <AppText variant="caption">Listed {relativeTime(l.createdAt)}</AppText>
      </View>
      <AppText weight="semibold" size={16}>
        {displayDecimal(l.price)} <AppText size={13} color={colors.muted}>{l.symbol}</AppText>
      </AppText>
      <Icon name="chevronRight" size={18} color={colors.muted} />
    </Pressable>
  );
}
