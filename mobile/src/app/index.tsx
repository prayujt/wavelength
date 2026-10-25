import { Redirect } from 'expo-router';
import { useSession } from '../lib/session';
export default function Index() {
  const session = useSession();
  return <Redirect href={session.grant || session.preview ? '/home' : '/welcome'} />;
}
