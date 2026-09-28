const boom = () => {
  throw new Error('ui-throws: render boom');
};
export default function activate() {
  return {
    badge: boom,
    sessionView: {
      banner: boom,
      panel: { title: 'Throws', component: boom },
    },
    settings: boom,
    commands: [{ id: 'boom', title: 'Throw from a command', run: boom }],
  };
}
