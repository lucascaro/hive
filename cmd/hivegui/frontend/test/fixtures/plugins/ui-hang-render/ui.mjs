const never = new Promise(() => {});
export default function activate() {
  return {
    settings: () => {
      throw never;
    },
  };
}
