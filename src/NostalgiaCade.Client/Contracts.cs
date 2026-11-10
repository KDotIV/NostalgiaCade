namespace NostalgiaCade.Client;

public sealed record CoreHello(int ProtocolVersion, string CoreVersion);
public sealed record Genre(string Id, string Name, int TargetEntries);
public sealed record CoreStatus(string Version, int ProtocolVersion, string Platform,
    string DatabaseVersion, int LocalGameCount, string Readiness, long UptimeSeconds, bool SetupRequired = false);
public sealed record LocalGame(string Id, string Title, string Path, string GenreId, string EmulatorId,
    string Ownership, string Scope, string Sha256, long Bytes, string ArtifactId);
public sealed record PlaySession(string Id, string GameId, int Pid, string State, int? ExitCode);
public sealed record CoreEndpoint(string PipeName, int ProcessId, int ProtocolVersion);

public sealed class CoreRpcException(int code, string message) : Exception(message)
{
    public int Code { get; } = code;
}
