import { redirect } from 'next/navigation';

export default function NewModule() {
  redirect('/explore?preset=new');
}
