import { Redirect, useLocalSearchParams } from 'expo-router';

export default function LibraryCatalogRedirect() {
  const { tab } = useLocalSearchParams<{ tab?: string }>();
  return <Redirect href={tab ? { pathname: '/library', params: { tab } } : '/library'} />;
}
