import { Redirect, router, useLocalSearchParams } from 'expo-router';
import { useSession } from '../../lib/session';
import { Screen, Empty } from '../../components/ui';
export default function Callback() {
  const session = useSession();
  const { code, error } = useLocalSearchParams<{ code?: string; error?: string }>();
  if (session.grant) return <Redirect href="/home" />;
  const issue = session.error || (!code && !error ? 'Start a new sign-in to continue.' : '');
  return (
    <Screen>
      <Empty
        title={issue ? 'Let’s try that again' : 'Finding your wavelength…'}
        text={issue || 'Finishing your secure connection.'}
        action={issue ? 'Back to sign-in' : undefined}
        onPress={() => router.replace('/welcome')}
      />
    </Screen>
  );
}
