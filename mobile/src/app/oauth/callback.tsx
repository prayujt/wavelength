import { Redirect } from 'expo-router';
import { useSession } from '../../lib/session';
import { Screen, Empty } from '../../components/ui';
export default function Callback() {
  const session = useSession();
  if (session.grant) return <Redirect href="/home" />;
  return (
    <Screen>
      <Empty
        title={session.error ? 'Let’s try that again' : 'Finding your wavelength…'}
        text={session.error || 'Finishing your secure connection.'}
      />
    </Screen>
  );
}
