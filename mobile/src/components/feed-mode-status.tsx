import { View } from 'react-native';
import { AudioLines, X } from 'lucide-react-native';
import { usePlayer } from '../lib/player';
import { color } from '../lib/theme';
import { IconButton, Press, Txt } from './ui';

export function FeedModeStatus() {
  const player = usePlayer();
  const failed = !player.playFeedEnabled && player.playFeedError && !!player.error;
  if ((!player.playFeedEnabled || player.track) && !failed) return null;
  return (
    <View
      testID="play-feed-mode"
      style={{
        backgroundColor: color.surface,
        borderBottomWidth: 1,
        borderBottomColor: color.line,
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
        <AudioLines size={18} color={failed ? color.danger : color.accent} />
        <View style={{ flex: 1, gap: 2, paddingVertical: 5 }} accessibilityLiveRegion="polite">
          <Txt size={13} weight="bold">
            {failed ? 'Play feed couldn’t start' : 'Starting play feed'}
          </Txt>
          <Txt size={11} numberOfLines={1} style={{ color: color.muted }}>
            {failed ? player.error : 'Choosing the first song…'}
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
    </View>
  );
}
