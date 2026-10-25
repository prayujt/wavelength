import React, { useEffect, useRef, useState } from 'react';
import { Switch, View } from 'react-native';
import { router, useLocalSearchParams } from 'expo-router';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowDown, ArrowUp, Check, Trash2, X } from 'lucide-react-native';
import { useAPI } from '../lib/session';
import { useTracks, useUser } from '../lib/queries';
import { moveItem } from '../core/queue';
import { color } from '../lib/theme';
import { Artwork } from '../components/artwork';
import {
  Button,
  Empty,
  Failure,
  Field,
  Header,
  IconButton,
  Loading,
  Screen,
  Txt,
} from '../components/ui';
import { useFeedback } from '../components/feedback';

export function PlaylistEditorScreen() {
  const { id, add } = useLocalSearchParams<{ id?: string; add?: string }>(),
    api = useAPI(),
    cache = useQueryClient(),
    feedback = useFeedback(),
    user = useUser();
  const [name, setName] = useState(''),
    [comment, setComment] = useState(''),
    [visible, setVisible] = useState(false),
    [ids, setIds] = useState<string[]>(add ? [add] : []);
  const initialized = useRef(false);
  const data = useQuery({
    queryKey: ['playlist', id],
    queryFn: () => api.user.getPlaylist({ id: id! }),
    enabled: !!id,
  });
  const tracks = useTracks(ids);
  useEffect(() => {
    const p = data.data?.playlist;
    if (p && !initialized.current) {
      initialized.current = true;
      setName(p.name);
      setComment(p.comment);
      setVisible(p.public);
      setIds(p.trackIds);
    }
  }, [data.data]);
  const own = !id || data.data?.playlist?.ownerId === user.data?.user?.id;
  async function save() {
    if (!name.trim()) return;
    let playlistId = id;
    if (!playlistId) {
      const result = await api.user.createPlaylist({ name: name.trim(), trackIds: ids });
      playlistId = result.playlist!.id;
    }
    await api.user.updatePlaylist({
      id: playlistId,
      name: name.trim(),
      comment,
      public: visible,
      replaceTracks: { ids },
    });
    await cache.invalidateQueries({ queryKey: ['playlists'] });
    await cache.invalidateQueries({ queryKey: ['playlist', playlistId] });
    feedback.notify(id ? 'Playlist updated' : 'Your playlist is ready');
    router.replace({ pathname: '/playlist/[id]', params: { id: playlistId } });
  }
  async function remove() {
    if (
      !id ||
      !(await feedback.confirm(
        'Delete this playlist?',
        'Your songs stay safely in the library. Only the playlist is removed.',
      ))
    )
      return;
    await api.user.deletePlaylist({ id });
    void cache.invalidateQueries({ queryKey: ['playlists'] });
    cache.removeQueries({ queryKey: ['playlist', id] });
    feedback.notify('Playlist deleted');
    router.replace('/library');
  }
  if (id && data.isPending)
    return (
      <Screen>
        <Loading />
      </Screen>
    );
  if (id && data.error)
    return (
      <Screen>
        <Failure error={data.error} retry={data.refetch} />
      </Screen>
    );
  if (!own)
    return (
      <Screen>
        <Header back title="This playlist belongs to another listener" />
        <Empty
          text="You can listen, but only the owner can edit this playlist."
          title="Made by someone else"
        />
      </Screen>
    );
  return (
    <Screen>
      <Header
        back
        title={id ? 'A few finishing touches.' : 'Make it your own.'}
        eyebrow={id ? 'EDIT PLAYLIST' : 'CREATE A PLAYLIST'}
        subtitle="For a feeling. A person. No reason at all."
      />
      <View style={{ paddingHorizontal: 24, gap: 22 }}>
        <View style={{ alignItems: 'center', paddingVertical: 8 }}>
          <Artwork
            title={name || 'Your next favorite mix'}
            id={id || 'new-playlist'}
            size={150}
            radius={20}
          />
        </View>
        <Field
          label="Playlist name"
          value={name}
          onChange={setName}
          placeholder="Give it a good name"
        />
        <Field
          label="A little about this playlist"
          value={comment}
          onChange={setComment}
          placeholder="What does it sound like?"
          multiline
        />
        <View
          style={{
            flexDirection: 'row',
            alignItems: 'center',
            justifyContent: 'space-between',
            padding: 17,
            backgroundColor: color.surface,
            borderRadius: 16,
            gap: 14,
          }}
        >
          <View style={{ flex: 1, gap: 7 }}>
            <Txt weight="medium">Let other listeners find it</Txt>
            <Txt muted size={12} style={{ lineHeight: 19 }}>
              Visible to other signed-in listeners. Public listening links are managed separately.
            </Txt>
          </View>
          <Switch
            accessibilityLabel="Playlist visibility"
            value={visible}
            onValueChange={setVisible}
            trackColor={{ false: color.line, true: '#64866C' }}
            thumbColor={visible ? color.accent : color.muted}
          />
        </View>
        <Button
          title={id ? 'Save changes' : 'Create playlist'}
          icon={Check}
          disabled={!name.trim()}
          onPress={save}
        />
      </View>
      {ids.length > 0 && (
        <View style={{ paddingHorizontal: 24, marginTop: 30 }}>
          <Txt size={21} weight="display" style={{ marginBottom: 17 }}>
            In the mix
          </Txt>
          <Txt size={12} muted style={{ marginBottom: 14 }}>
            Change the order. Leave only what belongs.
          </Txt>
          {tracks.isPending ? (
            <Loading />
          ) : (
            ids.map((trackId, i) => {
              const track = tracks.data?.find((t) => t.id === trackId);
              return (
                <View
                  key={`${trackId}:${i}`}
                  style={{
                    flexDirection: 'row',
                    alignItems: 'center',
                    minHeight: 76,
                    gap: 10,
                    borderBottomWidth: 1,
                    borderBottomColor: color.line,
                  }}
                >
                  <Txt size={11} muted style={{ width: 20 }}>
                    {i + 1}
                  </Txt>
                  {track && (
                    <Artwork
                      title={track.album}
                      id={track.albumId || track.album}
                      path={track.artworkPath}
                      size={40}
                      radius={7}
                    />
                  )}
                  <View style={{ flex: 1 }}>
                    <Txt size={12} weight="medium" numberOfLines={1}>
                      {track?.title || 'Unavailable song'}
                    </Txt>
                    {track && (
                      <Txt muted size={10} numberOfLines={1} style={{ marginTop: 4 }}>
                        {track.artist}
                      </Txt>
                    )}
                  </View>
                  <IconButton
                    icon={ArrowUp}
                    label={`Move song ${i + 1} up`}
                    onPress={() => setIds(moveItem(ids, i, i - 1))}
                    size={15}
                    disabled={!i}
                    style={{ width: 32 }}
                  />
                  <IconButton
                    icon={ArrowDown}
                    label={`Move song ${i + 1} down`}
                    onPress={() => setIds(moveItem(ids, i, i + 1))}
                    size={15}
                    disabled={i === ids.length - 1}
                    style={{ width: 32 }}
                  />
                  <IconButton
                    icon={X}
                    label={`Remove song ${i + 1}`}
                    onPress={() => setIds(ids.filter((_, index) => i !== index))}
                    size={16}
                    style={{ width: 32 }}
                  />
                </View>
              );
            })
          )}
        </View>
      )}
      {id && (
        <View style={{ margin: 24 }}>
          <Button
            title="Delete playlist"
            icon={Trash2}
            secondary
            onPress={remove}
            style={{ borderColor: '#72433A' }}
          />
        </View>
      )}
    </Screen>
  );
}
