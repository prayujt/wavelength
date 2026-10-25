import React, { useEffect, useState } from 'react';
import { View } from 'react-native';
import { Download, Trash2 } from 'lucide-react-native';
import { useDevice } from '../components/device';
import { Button, Empty, Header, IconButton, Screen, Txt } from '../components/ui';
import { TrackRow } from '../components/media';
import { usePlayer } from '../lib/player';
import { decodedTracks } from '../lib/device-library';
import { useFeedback } from '../components/feedback';
import { useSession } from '../lib/session';
import { entries, installed, partition } from '../lib/device-store';
const size = (bytes: number) => (bytes / 1024 / 1024).toFixed(1) + ' MB';
export function OnDeviceScreen() {
  const device = useDevice(),
    player = usePlayer(),
    feedback = useFeedback(),
    session = useSession();
  const [storage, setStorage] = useState(''),
    [pending, setPending] = useState(0),
    [failed, setFailed] = useState(0);
  useEffect(() => {
    if (!installed() || !session.account) return;
    void navigator.storage
      ?.estimate?.()
      .then((estimate) =>
        setStorage(
          estimate.quota
            ? `${size(estimate.usage || 0)} of ${size(estimate.quota)} used by this site`
            : 'Browser manages available storage',
        ),
      );
    void entries(partition(session.account) + 'outbox:').then((items) => setPending(items.length));
    void entries(partition(session.account) + 'failed:').then((items) => setFailed(items.length));
  }, [device.items, session.account]);
  if (!device.enabled)
    return (
      <Screen>
        <Header back title="On this device" />
        <Empty
          title="Install Wavelength first"
          text="Device downloads are available in the installed app. Open your browser’s install menu to add Wavelength."
        />
      </Screen>
    );
  const tracks = decodedTracks(device.items);
  return (
    <Screen>
      <Header back title="On this device" subtitle="Your music, even without a connection." />
      <View style={{ paddingHorizontal: 24, gap: 12, marginBottom: 20 }}>
        <Txt>
          {tracks.length} complete songs ·{' '}
          {size(device.items.reduce((sum, item) => sum + item.bytes, 0))}
        </Txt>
        <Txt size={12} muted>
          {storage}
        </Txt>
        <Txt size={12} muted>
          {pending ? `${pending} listening updates waiting to sync. ` : ''}Keep the app open until
          downloads finish. Your browser can reclaim storage; downloaded songs are checked when you
          open this screen.
        </Txt>
        {!!failed && (
          <Txt size={12} muted>
            {failed} history updates could not sync because their track or event is no longer
            available.
          </Txt>
        )}
        {!!device.error && <Txt size={12}>{device.error}</Txt>}
        {!!tracks.length && (
          <Button
            title="Play downloaded music"
            icon={Download}
            onPress={() => player.play(tracks)}
          />
        )}
        {!!tracks.length && (
          <Button
            title="Remove all device downloads"
            icon={Trash2}
            onPress={async () => {
              if (
                await feedback.confirm(
                  'Remove device downloads?',
                  'This removes saved audio from this device. Your server library, account and listening history stay available.',
                  'Remove downloads',
                )
              )
                for (const track of tracks) await device.remove(track.id);
            }}
          />
        )}
      </View>
      {!tracks.length && (
        <Empty
          title="Take your music with you"
          text="Save a song from its menu, or download an album or playlist. A song appears here after the whole file is saved."
        />
      )}
      {tracks.map((track, index) => (
        <TrackRow
          key={track.id}
          track={track}
          onPlay={() => player.play(tracks, index)}
          trailing={
            <IconButton
              icon={Trash2}
              label={`Remove ${track.title} from device`}
              onPress={() => device.remove(track.id)}
            />
          }
        />
      ))}
    </Screen>
  );
}
