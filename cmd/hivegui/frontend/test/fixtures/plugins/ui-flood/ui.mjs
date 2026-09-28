// Floods three ways at once: React updates, settings writes, and event
// subscriptions. The app must stay usable around it.
export default function activate(hive) {
  const { React } = hive;
  let n = 0;
  const writes = setInterval(() => {
    for (let i = 0; i < 50; i++) hive.settings.set({ n: n++ });
  }, 0);
  const subs = setInterval(() => {
    for (let i = 0; i < 50; i++) hive.on('session:event', () => {})();
  }, 0);
  function Flood() {
    const [tick, setTick] = React.useState(0);
    React.useEffect(() => {
      const t = setTimeout(() => setTick((x) => x + 1), 0);
      return () => clearTimeout(t);
    }, [tick]);
    return React.createElement('span', { id: 'ui-flood-tick' }, String(tick));
  }
  return {
    settings: Flood,
    badge: () => ({ text: `F${n % 10}` }),
    deactivate() {
      clearInterval(writes);
      clearInterval(subs);
    },
  };
}
