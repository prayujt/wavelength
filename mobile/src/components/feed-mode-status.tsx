import { View } from 'react-native';
import { Sparkles, X } from 'lucide-react-native';
import { LinearGradient } from 'expo-linear-gradient';
import { usePlayer } from '../lib/player';
import { color } from '../lib/theme';
import { IconButton, Press, Txt } from './ui';

export function FeedModeStatus() {
  const player = usePlayer();
  const failed = !player.playFeedEnabled && player.playFeedError && !!player.error;
  if (!player.playFeedEnabled && !failed) return null;
  return (
    <LinearGradient
      testID="play-feed-mode"
      colors={failed ? ['#382A24', '#241F1B'] : ['#244A33', '#142A22']}
      start={{ x: 0, y: 0 }}
      end={{ x: 1, y: 1 }}
      style={{
        borderBottomWidth: 1,
        borderBottomColor: failed ? color.danger : '#C7F3CB40',
        boxShadow: '0px 5px 18px #C7F3CB18',
      }}
    >
      <View
        style={{
          flexDirection: 'row',
          alignItems: 'center',
          gap: 9,
          paddingLeft: 16,
          paddingRight: 8,
          minHeight: 48,
        }}
      >
        <Sparkles size={18} color={color.accent} />
        <View style={{ flex: 1, gap: 2, paddingVertical: 5 }} accessibilityLiveRegion="polite">
          <Txt size={13} weight="bold">
            {failed ? 'Play feed couldn’t start' : 'Play feed is on'}
          </Txt>
          <Txt size={11} numberOfLines={1} style={{ color: color.muted }}>
            {failed
              ? player.error
              : player.startingFeed
                ? 'Choosing your first song…'
                : player.playFeedLoading
                  ? 'Choosing what comes next…'
                  : player.playFeedError
                    ? 'Suggestions will retry when connected'
                    : 'Adapts as you listen'}
          </Txt>
        </View>
        {failed && (
          <Press
            label="Retry play feed"
            onPress={() => player.startFeed()}
            style={{ minHeight: 44, paddingHorizontal: 8, justifyContent: 'center' }}
          >
            <Txt size={12} weight="bold">
              Retry
            </Txt>
          </Press>
        )}
        {failed ? (
          <IconButton
            icon={X}
            label="Dismiss play feed error"
            onPress={player.stopFeed}
            size={17}
          />
        ) : (
          <Press
            label="Turn off play feed"
            onPress={player.stopFeed}
            style={{ minHeight: 44, paddingHorizontal: 10, justifyContent: 'center' }}
          >
            <Txt size={12} weight="bold" style={{ color: color.accent }}>
              Turn off
            </Txt>
          </Press>
        )}
      </View>
    </LinearGradient>
  );
}
